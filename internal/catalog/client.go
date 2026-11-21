// Package catalog provides the community catalog client.
// The catalog is a public GitHub repo containing API connector metadata,
// Starlark workflows, and WASM transforms.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultCatalogRepo = "https://raw.githubusercontent.com/totalwindupflightsystems/musterflow-catalog/main"

// catalogRepoEnvVar overrides the community catalog repo URL. It exists so an
// operator (or a test, or this binary's own verification run) can point the
// catalog at a mirror — or at an unreachable host to exercise the bundled
// fallback — without rebuilding.
const catalogRepoEnvVar = "MUSTERFLOW_CATALOG_REPO_URL"

// ErrBackendUnavailable classifies the failure where the catalog backend
// itself is absent: the index URL answers 404 (repo missing, private, or
// renamed). It carries no message text of its own in the wrapped error — the
// sentence operators already see ("catalog backend not available (HTTP 404):
// repo … absent or private") is composed at the 404 site, so classifying on
// errors.Is changes the failure CLASS without changing a byte of the text.
//
// Callers use it to answer with a 404-class status instead of a 500: a missing
// backend is a not-found condition, not a server error (DF-035).
var ErrBackendUnavailable = errors.New("catalog backend not available")

// CatalogEntry represents an item in the community catalog.
type CatalogEntry struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"` // "api", "workflow", "wasm"
	Description string    `json:"description"`
	SpecURL     string    `json:"spec_url,omitempty"`
	Author      string    `json:"author"`
	Version     string    `json:"version"`
	Score       int       `json:"score"`        // 0-10
	QualityTier string    `json:"quality_tier"` // "official", "community-inferred", "untested"
	AddedAt     time.Time `json:"added_at"`
	Downloads   int       `json:"downloads"`
}

// Client interacts with the community catalog.
type Client struct {
	repoURL    string
	httpClient *http.Client
}

// NewClient creates a new catalog client. The repo URL can be overridden with
// MUSTERFLOW_CATALOG_REPO_URL.
func NewClient() *Client {
	repoURL := defaultCatalogRepo
	if override := strings.TrimSpace(os.Getenv(catalogRepoEnvVar)); override != "" {
		repoURL = override
	}
	return &Client{
		repoURL:    repoURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// NewClientWithRepoURL creates a catalog client pointing at a custom repo URL.
// Used for testing with httptest.Server.
func NewClientWithRepoURL(repoURL string) *Client {
	return &Client{
		repoURL:    repoURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Search searches the catalog index for matching entries.
func (c *Client) Search(query string) ([]CatalogEntry, error) {
	entries, err := c.List()
	if err != nil {
		return nil, err
	}

	// Fuzzy search with relevance scoring (see search.go)
	return Search(entries, query), nil
}

// List fetches every entry in the community catalog index, unfiltered.
func (c *Client) List() ([]CatalogEntry, error) {
	url := fmt.Sprintf("%s/index.json", c.repoURL)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch catalog index: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w (HTTP %d): repo %s absent or private", ErrBackendUnavailable, resp.StatusCode, c.repoURL)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog returned status %d", resp.StatusCode)
	}

	var entries []CatalogEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	return entries, nil
}

// FetchEntry retrieves a specific catalog entry.
func (c *Client) FetchEntry(id string) (*CatalogEntry, []byte, error) {
	url := fmt.Sprintf("%s/entries/%s.json", c.repoURL, id)
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch entry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
