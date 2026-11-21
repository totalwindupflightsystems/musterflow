package catalog

import (
	"testing"
	"time"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

func TestConnectionToCatalogEntry(t *testing.T) {
	now := time.Now()
	conn := &app.APIConnection{
		ID:            "test-api",
		Name:          "Test API",
		SpecURL:       "https://example.com/spec.json",
		BaseURL:       "https://api.example.com",
		Version:       "2.1.0",
		Description:   "A test API for unit testing",
		AuthType:      "bearer",
		AddedAt:       now,
		UpdatedAt:     now,
		EndpointCount: 15,
	}

	entry := ConnectionToCatalogEntry(conn)

	if entry.ID != conn.ID {
		t.Errorf("ID: got %q, want %q", entry.ID, conn.ID)
	}
	if entry.Name != conn.Name {
		t.Errorf("Name: got %q, want %q", entry.Name, conn.Name)
	}
	if entry.Type != "api" {
		t.Errorf("Type: got %q, want \"api\"", entry.Type)
	}
	if entry.Description != conn.Description {
		t.Errorf("Description: got %q, want %q", entry.Description, conn.Description)
	}
	if entry.SpecURL != conn.SpecURL {
		t.Errorf("SpecURL: got %q, want %q", entry.SpecURL, conn.SpecURL)
	}
	if entry.Author != "community" {
		t.Errorf("Author: got %q, want \"community\"", entry.Author)
	}
	if entry.Version != conn.Version {
		t.Errorf("Version: got %q, want %q", entry.Version, conn.Version)
	}
	if !entry.AddedAt.Equal(conn.AddedAt) {
		t.Errorf("AddedAt: got %v, want %v", entry.AddedAt, conn.AddedAt)
	}
	if entry.Downloads != 0 {
		t.Errorf("Downloads: got %d, want 0", entry.Downloads)
	}
	// The tier and score come from the catalog scoring function (CATALOG-002),
	// not from a hardcoded constant: this fixture has a description (>20 chars),
	// a version and a spec URL on a third-party host, so Score() lands on 4 —
	// the structured-spec bonus (+2, an "api" entry that points at a spec
	// document) plus description and version — which is below scoreToTier's 7
	// threshold because the spec host is not the provider's own.
	if entry.Score != 4 {
		t.Errorf("Score: got %d, want 4", entry.Score)
	}
	if entry.BaseURL != conn.BaseURL {
		t.Errorf("BaseURL: got %q, want %q", entry.BaseURL, conn.BaseURL)
	}
	if entry.QualityTier != "community-inferred" {
		t.Errorf("QualityTier: got %q, want %q", entry.QualityTier, scoreToTier(4))
	}
}

func TestConnectionToCatalogEntryScoreIsComputed(t *testing.T) {
	tests := []struct {
		name      string
		conn      *app.APIConnection
		wantScore int
		wantTier  string
	}{
		{
			name: "official host plus metadata",
			conn: &app.APIConnection{
				ID:          "gh",
				Name:        "GitHub",
				SpecURL:     "https://api.github.com/openapi.json",
				Version:     "1.0.0",
				Description: "The GitHub REST API for repos, issues and PRs",
			},
			// +5 official provider domain (api.github.com is GitHub's), +2
			// structured spec (an "api" entry pointing at a spec document),
			// +1 description, +1 version (CATALOG-005).
			wantScore: 9,
			wantTier:  "official",
		},
		{
			name: "short fallback description scores only on the version",
			conn: &app.APIConnection{
				ID:   "bare",
				Name: "A", // "API connector for A" is 19 chars: no bonus
			},
			wantScore: 1,
			wantTier:  "untested",
		},
		{
			name: "community host with description and version",
			conn: &app.APIConnection{
				ID:          "community",
				Name:        "Community",
				SpecURL:     "https://example.com/spec.json",
				Version:     "2.1.0",
				Description: "A test API for unit testing",
			},
			// +2 structured spec, +1 description, +1 version (CATALOG-005).
			wantScore: 4,
			wantTier:  "community-inferred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := ConnectionToCatalogEntry(tt.conn)
			if entry.Score != tt.wantScore {
				t.Errorf("Score: got %d, want %d", entry.Score, tt.wantScore)
			}
			if entry.QualityTier != tt.wantTier {
				t.Errorf("QualityTier: got %q, want %q", entry.QualityTier, tt.wantTier)
			}
			// A pushed entry must never carry the old unscored placeholder, and
			// its tier must be the one its own score maps to.
			if entry.Score == 0 && entry.Type != "" && entry.Version == "" && entry.QualityTier == "community-inferred" {
				t.Errorf("entry kept the pre-CATALOG-002 placeholder score/tier: %+v", entry)
			}
			if want := scoreToTier(entry.Score); entry.QualityTier != want {
				t.Errorf("tier/score disagree: score %d maps to %q, entry says %q", entry.Score, want, entry.QualityTier)
			}
		})
	}
}

func TestConnectionToCatalogEntryEmptyDescription(t *testing.T) {
	conn := &app.APIConnection{
		ID:   "myapi",
		Name: "MyAPI",
	}
	entry := ConnectionToCatalogEntry(conn)
	want := "API connector for MyAPI"
	if entry.Description != want {
		t.Errorf("Description fallback: got %q, want %q", entry.Description, want)
	}
}

func TestConnectionToCatalogEntryEmptyVersion(t *testing.T) {
	conn := &app.APIConnection{
		ID:   "myapi",
		Name: "MyAPI",
	}
	entry := ConnectionToCatalogEntry(conn)
	if entry.Version != "1.0.0" {
		t.Errorf("Version fallback: got %q, want \"1.0.0\"", entry.Version)
	}
}

func TestConnectionToCatalogEntryTypeAlwaysAPI(t *testing.T) {
	conns := []*app.APIConnection{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B", Description: "has desc"},
		{ID: "c", Name: "C", Version: "3.0.0"},
	}
	for _, conn := range conns {
		entry := ConnectionToCatalogEntry(conn)
		if entry.Type != "api" {
			t.Errorf("Type for %s: got %q, want \"api\"", conn.ID, entry.Type)
		}
	}
}

func TestConnectionToCatalogEntryAuthorAlwaysCommunity(t *testing.T) {
	conns := []*app.APIConnection{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B", Description: "has desc"},
	}
	for _, conn := range conns {
		entry := ConnectionToCatalogEntry(conn)
		if entry.Author != "community" {
			t.Errorf("Author for %s: got %q, want \"community\"", conn.ID, entry.Author)
		}
	}
}
