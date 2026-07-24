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
	if entry.QualityTier != "community-inferred" {
		t.Errorf("QualityTier: got %q, want \"community-inferred\"", entry.QualityTier)
	}
	if !entry.AddedAt.Equal(conn.AddedAt) {
		t.Errorf("AddedAt: got %v, want %v", entry.AddedAt, conn.AddedAt)
	}
	if entry.Downloads != 0 {
		t.Errorf("Downloads: got %d, want 0", entry.Downloads)
	}
	if entry.Score != 0 {
		t.Errorf("Score: got %d, want 0", entry.Score)
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
