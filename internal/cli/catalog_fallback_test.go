package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
)

// bundledSeedIDs mirrors catalog/seed.json: a fresh binary must still be able to
// offer at least these ten entries with the community index unreachable.
var bundledSeedIDs = []string{
	"github", "stripe", "slack", "discord", "openai",
	"notion", "linear", "jira", "twilio", "cloudflare",
}

// absentCatalogBackend is a community catalog repo that answers 404 everywhere —
// the shape of a missing, private, or renamed repo.
func absentCatalogBackend(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// runCatalog executes a catalog subcommand with stdout/stderr captured, so the
// assertions see exactly what a user would.
func runCatalog(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	root := NewRootCommand(r)
	out := new(bytes.Buffer)
	errOut := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func TestCatalogListServesBundledSeedsWhenIndexUnavailable(t *testing.T) {
	t.Setenv("MUSTERFLOW_CATALOG_REPO_URL", absentCatalogBackend(t))

	out, errOut, err := runCatalog(t, "catalog", "list")
	if err != nil {
		t.Fatalf("catalog list with an unavailable community index must serve bundled seeds, got: %v", err)
	}

	// The notice has to be visible, not silent, and greppable.
	if !strings.Contains(errOut, "community index") || !strings.Contains(errOut, "bundled") {
		t.Errorf("expected a fallback notice naming the community index and bundled entries, got stderr %q", errOut)
	}

	missing := make([]string, 0, len(bundledSeedIDs))
	for _, id := range bundledSeedIDs {
		if !strings.Contains(out, id) {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Errorf("catalog list served %d of %d bundled ids; missing %v\noutput:\n%s",
			len(bundledSeedIDs)-len(missing), len(bundledSeedIDs), missing, out)
	}
	if !strings.Contains(out, "Catalog entries (") {
		t.Errorf("expected the catalog listing header, got:\n%s", out)
	}
}

func TestCatalogListServesCommunityWhenReachable(t *testing.T) {
	index := []catalog.CatalogEntry{
		{ID: "community-one", Name: "Community One", Type: "api", Description: "first"},
		{ID: "community-two", Name: "Community Two", Type: "workflow", Description: "second"},
	}
	body, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/index.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MUSTERFLOW_CATALOG_REPO_URL", srv.URL)

	out, errOut, execErr := runCatalog(t, "catalog", "list")
	if execErr != nil {
		t.Fatalf("catalog list against a reachable index: %v", execErr)
	}
	if !strings.Contains(out, "community-one") || !strings.Contains(out, "community-two") {
		t.Errorf("expected the community entries, got:\n%s", out)
	}
	if strings.Contains(out, "github") {
		t.Errorf("community-served listing must not carry bundled seed entries:\n%s", out)
	}
	if strings.Contains(errOut, "bundled") {
		t.Errorf("a reachable community index must not print the fallback notice, got %q", errOut)
	}
}

func TestCatalogPullUnknownIDStillReportsNotFound(t *testing.T) {
	// The bundled fallback must not manufacture entries: an id that is in
	// neither catalog keeps the pre-existing not-found error.
	t.Setenv("MUSTERFLOW_CATALOG_REPO_URL", absentCatalogBackend(t))

	_, _, err := runCatalog(t, "catalog", "pull", "no-such-api")
	if err == nil {
		t.Fatal("expected an error pulling an id that exists in neither catalog")
	}
	if !strings.Contains(err.Error(), "no-such-api") {
		t.Errorf("not-found error must name the id: %v", err)
	}
}
