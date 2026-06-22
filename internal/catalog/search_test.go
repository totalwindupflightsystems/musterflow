package catalog

import (
	"testing"
	"time"
)

func TestSearch(t *testing.T) {
	now := time.Now()
	entries := []CatalogEntry{
		{ID: "petstore", Name: "Petstore", Description: "A sample pet store API", AddedAt: now},
		{ID: "github", Name: "GitHub API", Description: "GitHub REST API v3", AddedAt: now},
		{ID: "stripe", Name: "Stripe", Description: "Payment processing API", AddedAt: now},
		{ID: "petfinder", Name: "Petfinder", Description: "Find pets near you", AddedAt: now},
		{ID: "weather", Name: "Weather API", Description: "Weather forecasts", AddedAt: now},
	}

	tests := []struct {
		name        string
		query       string
		wantIDs     []string
		wantMinLen  int
		wantExact   bool // first result should be this ID
	}{
		{
			name:      "exact name match",
			query:     "Petstore",
			wantIDs:   []string{"petstore"},
			wantMinLen: 1,
		},
		{
			name:      "prefix match",
			query:     "Pet",
			wantIDs:   []string{"petstore", "petfinder"},
			wantMinLen: 2,
		},
		{
			name:      "contains in name",
			query:     "stripe",
			wantIDs:   []string{"stripe"},
			wantMinLen: 1,
		},
		{
			name:      "no match",
			query:     "xyznonexistent",
			wantMinLen: 0,
		},
		{
			name:      "description only match",
			query:     "payment",
			wantIDs:   []string{"stripe"},
			wantMinLen: 1,
		},
		{
			name:      "ID match",
			query:     "github",
			wantIDs:   []string{"github"},
			wantMinLen: 1,
		},
		{
			name:       "sorting by score - exact beats contains",
			query:      "pet",
			wantExact:  true,
			wantMinLen: 2,
		},
		{
			name:      "empty query returns nil",
			query:     "",
			wantMinLen: 0,
		},
		{
			name:      "case insensitive - uppercase query",
			query:     "PETSTORE",
			wantIDs:   []string{"petstore"},
			wantMinLen: 1,
		},
		{
			name:      "case insensitive - mixed case query",
			query:     "StRiPe",
			wantIDs:   []string{"stripe"},
			wantMinLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := Search(entries, tt.query)
			if len(results) < tt.wantMinLen {
				t.Fatalf("got %d results, want at least %d", len(results), tt.wantMinLen)
			}
			if len(tt.wantIDs) > 0 {
				gotIDs := make(map[string]bool)
				for _, r := range results {
					gotIDs[r.ID] = true
				}
				for _, wantID := range tt.wantIDs {
					if !gotIDs[wantID] {
						t.Errorf("expected ID %q in results, not found. Results: %v", wantID, resultIDs(results))
					}
				}
			}
			// Verify sorting: scores should be descending
			for i := 1; i < len(results); i++ {
				if results[i].Score > results[i-1].Score {
					t.Errorf("results not sorted by score descending: index %d (score %d) > index %d (score %d)",
						i, results[i].Score, i-1, results[i-1].Score)
				}
			}
		})
	}
}

func TestSearchEmptyCatalog(t *testing.T) {
	results := Search(nil, "anything")
	if results != nil {
		t.Errorf("expected nil for empty catalog, got %v", results)
	}

	results = Search([]CatalogEntry{}, "anything")
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty catalog, got %d", len(results))
	}
}

func TestSearchExactMatchScoresHighest(t *testing.T) {
	entries := []CatalogEntry{
		{ID: "a", Name: "pet", Description: "something about pets"},
		{ID: "b", Name: "petstore", Description: "pet store"},
	}
	results := Search(entries, "pet")
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// Exact match "pet" should score higher than "petstore" (contains)
	if results[0].ID != "a" {
		t.Errorf("expected exact match 'a' first, got %q (score %d)", results[0].ID, results[0].Score)
	}
}

func resultIDs(entries []CatalogEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}