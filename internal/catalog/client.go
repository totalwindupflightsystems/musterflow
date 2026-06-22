// Package catalog provides the community catalog client.
// The catalog is a public GitHub repo containing API connector metadata,
// Starlark workflows, and WASM transforms.
package catalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultCatalogRepo = "https://raw.githubusercontent.com/totalwindupflightsystems/musterflow-catalog/main"

// CatalogEntry represents an item in the community catalog.
type CatalogEntry struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"` // "api", "workflow", "wasm"
	Description string    `json:"description"`
	SpecURL     string    `json:"spec_url,omitempty"`
	Author      string    `json:"author"`
	Version     string    `json:"version"`
	Score       int       `json:"score"` // 0-10
	QualityTier  string   `json:"quality_tier"` // "official", "community-inferred", "untested"
	AddedAt     time.Time `json:"added_at"`
	Downloads   int       `json:"downloads"`
}

// Client interacts with the community catalog.
type Client struct {
	repoURL  string
	httpClient *http.Client
}

// NewClient creates a new catalog client.
func NewClient() *Client {
	return &Client{
		repoURL: defaultCatalogRepo,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewClientWithRepoURL creates a catalog client pointing at a custom repo URL.
// Used for testing with httptest.Server.
func NewClientWithRepoURL(repoURL string) *Client {
	return &Client{
		repoURL:  repoURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Search searches the catalog index for matching entries.
func (c *Client) Search(query string) ([]CatalogEntry, error) {
	url := fmt.Sprintf("%s/index.json", c.repoURL)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch catalog index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // catalog may not exist yet
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog returned status %d", resp.StatusCode)
	}

	var entries []CatalogEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}

	// Fuzzy search with relevance scoring (see search.go)
	return Search(entries, query), nil
}

// FetchEntry retrieves a specific catalog entry.
func (c *Client) FetchEntry(id string) (*CatalogEntry, []byte, error) {
	url := fmt.Sprintf("%s/entries/%s.json", c.repoURL, id)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch entry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("entry %s not found (status %d)", id, resp.StatusCode)
	}

	var entry CatalogEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, nil, fmt.Errorf("decode entry: %w", err)
	}
	return &entry, nil, nil
}

// ToJSON serializes a catalog entry so it can be pushed to the catalog repo.
func (e *CatalogEntry) ToJSON() ([]byte, error) {
	return json.MarshalIndent(e, "", "  ")
}

func contains(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}
