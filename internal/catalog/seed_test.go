package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedIDs are the ids the bundled catalog must carry. The acceptance for
// CATALOG-006 is "a fresh binary still offers a usable catalog", which needs at
// least ten entries — spelled out here so an accidental truncation of the
// embedded copy fails loudly.
var seedIDs = []string{
	"github", "stripe", "slack", "discord", "openai",
	"notion", "linear", "jira", "twilio", "cloudflare",
}

// unreachableBackend returns a repo URL that fails at the transport layer:
// a listener that was closed before use.
func unreachableBackend(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

// absentBackend returns a repo URL that answers 404 for every path, which is the
// shape of a missing/private/renamed community catalog repo.
func absentBackend(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// presentBackend returns a repo URL whose index.json carries entries and whose
// /entries/<id>.json paths serve them, i.e. a healthy community catalog.
func presentBackend(t *testing.T, entries []CatalogEntry) string {
	t.Helper()
	index, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(index)
			return
		}
		if id, ok := strings.CutPrefix(r.URL.Path, "/entries/"); ok {
			id = strings.TrimSuffix(id, ".json")
			for _, e := range entries {
				if e.ID != id {
					continue
				}
				body, err := json.Marshal(e)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestBundledEntriesAreUsable(t *testing.T) {
	entries, err := BundledEntries()
	if err != nil {
		t.Fatalf("BundledEntries: %v", err)
	}
	if len(entries) < len(seedIDs) {
		t.Fatalf("bundled catalog has %d entries, want at least %d", len(entries), len(seedIDs))
	}
	byID := make(map[string]CatalogEntry, len(entries))
	for _, e := range entries {
		if e.ID == "" || e.Name == "" || e.Description == "" {
			t.Errorf("bundled entry is missing id/name/description: %+v", e)
		}
		if _, dup := byID[e.ID]; dup {
			t.Errorf("bundled catalog carries %q twice", e.ID)
		}
		byID[e.ID] = e
	}
	for _, id := range seedIDs {
		e, ok := byID[id]
		if !ok {
			t.Errorf("bundled catalog is missing %q", id)
			continue
		}
		if e.SpecURL == "" {
			t.Errorf("bundled entry %q carries no spec_url, so `catalog pull %s` cannot resolve a spec", id, id)
		}
	}
}

func TestBundledSeedMatchesRepoSeed(t *testing.T) {
	// internal/catalog/seed.json is a copy of the repo's tracked
	// catalog/seed.json because go:embed cannot reach outside a package
	// directory. This test is the drift guard: if one copy gains an entry, the
	// other must too.
	repoSeed, err := os.ReadFile(filepath.Join("..", "..", "..", "catalog", "seed.json"))
	if err != nil {
		if os.IsNotExist(err) {
			t.Skip("repo seed catalog not present in this tree (built outside the repo)")
		}
		t.Fatalf("read repo seed catalog: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(repoSeed), bytes.TrimSpace(seedJSON)) {
		t.Fatalf("bundled catalog drifted from catalog/seed.json:\n embedded: %d bytes\n repo:     %d bytes\n"+
			"copy catalog/seed.json over internal/catalog/seed.json to resync",
			len(bytes.TrimSpace(seedJSON)), len(bytes.TrimSpace(repoSeed)))
	}
}

func TestSearchCatalogFallsBackWhenBackendAbsent(t *testing.T) {
	client := NewClientWithRepoURL(absentBackend(t))

	res, err := client.SearchCatalog("github")
	if err != nil {
		t.Fatalf("SearchCatalog: %v", err)
	}
	if res.Source != SourceBundled {
		t.Fatalf("source = %q, want %q", res.Source, SourceBundled)
	}
	if res.Reason == nil {
		t.Error("bundled answer must carry the community-side reason")
	} else if !errors.Is(res.Reason, ErrBackendUnavailable) {
		t.Errorf("reason lost its backend-unavailable class: %v", res.Reason)
	}
	if len(res.Entries) == 0 || res.Entries[0].ID != "github" {
		t.Fatalf("expected the bundled github entry, got %+v", res.Entries)
	}
}

func TestSearchCatalogFallsBackWhenIndexUnreachable(t *testing.T) {
	client := NewClientWithRepoURL(unreachableBackend(t))

	res, err := client.SearchCatalog("stripe")
	if err != nil {
		t.Fatalf("SearchCatalog: %v", err)
	}
	if res.Source != SourceBundled {
		t.Fatalf("source = %q, want %q", res.Source, SourceBundled)
	}
	if res.Reason == nil {
		t.Error("bundled answer must carry the transport failure as its reason")
	}
	if len(res.Entries) == 0 || res.Entries[0].ID != "stripe" {
		t.Fatalf("expected the bundled stripe entry, got %+v", res.Entries)
	}
}

func TestSearchCatalogServesCommunityWhenReachable(t *testing.T) {
	client := NewClientWithRepoURL(presentBackend(t, []CatalogEntry{{
		ID:          "community-api",
		Name:        "Community API",
		Type:        "api",
		Description: "only the community index has this",
	}}))

	res, err := client.SearchCatalog("community")
	if err != nil {
		t.Fatalf("SearchCatalog: %v", err)
	}
	if res.Source != SourceCommunity {
		t.Fatalf("source = %q, want %q", res.Source, SourceCommunity)
	}
	if res.Reason != nil {
		t.Errorf("a reachable community index must not report a fallback reason: %v", res.Reason)
	}
	if len(res.Entries) != 1 || res.Entries[0].ID != "community-api" {
		t.Fatalf("expected only the community entry, got %+v", res.Entries)
	}
}

func TestListCatalogFallsBackToBundledSeeds(t *testing.T) {
	res, err := NewClientWithRepoURL(absentBackend(t)).ListCatalog()
	if err != nil {
		t.Fatalf("ListCatalog: %v", err)
	}
	if res.Source != SourceBundled {
		t.Fatalf("source = %q, want %q", res.Source, SourceBundled)
	}
	if len(res.Entries) < len(seedIDs) {
		t.Fatalf("listed %d bundled entries, want at least %d", len(res.Entries), len(seedIDs))
	}
	seen := make(map[string]bool, len(res.Entries))
	for _, e := range res.Entries {
		seen[e.ID] = true
	}
	for _, id := range seedIDs {
		if !seen[id] {
			t.Errorf("bundled listing is missing %q", id)
		}
	}
}

func TestListCatalogServesCommunityWhenReachable(t *testing.T) {
	index := []CatalogEntry{
		{ID: "one", Name: "One", Type: "api"},
		{ID: "two", Name: "Two", Type: "workflow"},
	}
	res, err := NewClientWithRepoURL(presentBackend(t, index)).ListCatalog()
	if err != nil {
		t.Fatalf("ListCatalog: %v", err)
	}
	if res.Source != SourceCommunity {
		t.Fatalf("source = %q, want %q", res.Source, SourceCommunity)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("listed %d community entries, want 2: %+v", len(res.Entries), res.Entries)
	}
	for _, e := range res.Entries {
		if e.ID == "github" {
			t.Error("community listing leaked bundled seed entries")
		}
	}
}

func TestFetchCatalogEntryFallsBackToBundled(t *testing.T) {
	res, err := NewClientWithRepoURL(absentBackend(t)).FetchCatalogEntry("github")
	if err != nil {
		t.Fatalf("FetchCatalogEntry: %v", err)
	}
	if res.Source != SourceBundled {
		t.Fatalf("source = %q, want %q", res.Source, SourceBundled)
	}
	if res.Entry == nil || res.Entry.ID != "github" {
		t.Fatalf("expected the bundled github entry, got %+v", res.Entry)
	}
	if res.Entry.SpecURL == "" {
		t.Error("bundled entry lost its spec_url")
	}
}

func TestFetchCatalogEntryPrefersCommunity(t *testing.T) {
	res, err := NewClientWithRepoURL(presentBackend(t, []CatalogEntry{{
		ID:   "community-api",
		Name: "Community API",
	}})).FetchCatalogEntry("community-api")
	if err != nil {
		t.Fatalf("FetchCatalogEntry: %v", err)
	}
	if res.Source != SourceCommunity {
		t.Fatalf("source = %q, want %q", res.Source, SourceCommunity)
	}
	if res.Entry == nil || res.Entry.ID != "community-api" {
		t.Fatalf("expected the community entry, got %+v", res.Entry)
	}
}

func TestFetchCatalogEntryMissIsNotASilentSuccess(t *testing.T) {
	// A miss in both catalogs keeps the community index's own not-found error,
	// which is what `catalog pull` has always printed for an unknown id.
	res, err := NewClientWithRepoURL(absentBackend(t)).FetchCatalogEntry("no-such-api")
	if err == nil {
		t.Fatalf("expected a not-found error for an unknown id, got entry %+v", res.Entry)
	}
	if !strings.Contains(err.Error(), "no-such-api") {
		t.Errorf("not-found error must name the id: %v", err)
	}
	if res.Entry != nil {
		t.Errorf("expected no entry alongside the error, got %+v", res.Entry)
	}
}

func TestNewClientHonorsRepoURLEnv(t *testing.T) {
	t.Setenv(catalogRepoEnvVar, "http://catalog.example.invalid/mirror")
	if got := NewClient().repoURL; got != "http://catalog.example.invalid/mirror" {
		t.Errorf("NewClient repoURL = %q, want the %s override", got, catalogRepoEnvVar)
	}

	t.Setenv(catalogRepoEnvVar, "   ")
	if got := NewClient().repoURL; got != defaultCatalogRepo {
		t.Errorf("blank override must fall back to the default repo, got %q", got)
	}
}
