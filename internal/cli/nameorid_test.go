package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// nameOrIDSpecDoc is the document `refresh` re-fetches. The version differs from
// the seeded connection's so the assertion proves the spec was actually
// re-parsed, not just that a lookup succeeded.
const nameOrIDSpecDoc = `{
	"openapi": "3.0.0",
	"info": {"title": "Echo Test API", "version": "2.0.0"},
	"paths": {
		"/echo": {"get": {"operationId": "getEcho"}},
		"/ping": {"get": {"operationId": "getPing"}}
	}
}`

// newNameOrIDRegistry seeds one connected API whose id ("echo-test-api-2026")
// differs from its name ("echo-test-api") — the exact shape that made the
// id-only lookups reject a name with a bare "not found" (DF-024).
func newNameOrIDRegistry(t *testing.T, specURL string) *app.Registry {
	t.Helper()

	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if err := r.Add(&app.APIConnection{
		ID:            "echo-test-api-2026",
		Name:          "echo-test-api",
		SpecURL:       specURL,
		BaseURL:       specURL,
		Version:       "1.0.0",
		EndpointCount: 0,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Neither a store-open failure nor a dashboard route is part of this test:
	// the local, id-or-name resolution path is what DF-024 is about.
	SetStoreError(nil)
	SetDashboardURL("")
	t.Cleanup(func() { SetDashboardURL("") })

	return r
}

// newNameOrIDSpecServer serves nameOrIDSpecDoc for the refresh path.
func newNameOrIDSpecServer(t *testing.T) *httptest.Server {
	t.Helper()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nameOrIDSpecDoc))
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TestRefreshResolvesAPIByNameOrID covers both accepted spellings on the local
// refresh path. Before DF-024 only the id worked; the name failed with
// "get api: not found".
func TestRefreshResolvesAPIByNameOrID(t *testing.T) {
	ts := newNameOrIDSpecServer(t)

	cases := []struct {
		label string
		arg   string
	}{
		{label: "by-name", arg: "echo-test-api"},
		{label: "by-id", arg: "echo-test-api-2026"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			r := newNameOrIDRegistry(t, ts.URL)

			out, err := runCLI(t, r, "refresh", tc.arg)
			if err != nil {
				t.Fatalf("refresh %q returned %v; output:\n%s", tc.arg, err, out)
			}
			for _, want := range []string{"✓ Refreshed echo-test-api", "1.0.0 → 2.0.0"} {
				if !strings.Contains(out, want) {
					t.Errorf("refresh %q output missing %q:\n%s", tc.arg, want, out)
				}
			}
		})
	}
}

// TestCatalogPushResolvesAPIByNameOrID covers both accepted spellings on the
// local catalog push path, which used the id-only registry.Get before DF-024.
func TestCatalogPushResolvesAPIByNameOrID(t *testing.T) {
	cases := []struct {
		label string
		arg   string
	}{
		{label: "by-name", arg: "echo-test-api"},
		{label: "by-id", arg: "echo-test-api-2026"},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			r := newNameOrIDRegistry(t, "https://example.invalid/openapi.json")

			out, err := runCLI(t, r, "catalog", "push", tc.arg)
			if err != nil {
				t.Fatalf("catalog push %q returned %v; output:\n%s", tc.arg, err, out)
			}
			for _, want := range []string{
				"Catalog entry JSON for echo-test-api-2026",
				`"id": "echo-test-api-2026"`,
			} {
				if !strings.Contains(out, want) {
					t.Errorf("catalog push %q output missing %q:\n%s", tc.arg, want, out)
				}
			}
		})
	}
}

// TestUnknownArgErrorNamesConnectedAPIs is the actionable-failure half of
// DF-024: a miss must list the connected id (and name) and must not be wrapped
// by anything that hides the registry's message.
func TestUnknownArgErrorNamesConnectedAPIs(t *testing.T) {
	commands := [][]string{
		{"refresh", "nope"},
		{"catalog", "push", "nope"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			r := newNameOrIDRegistry(t, "https://example.invalid/openapi.json")

			out, err := runCLI(t, r, args...)
			if err == nil {
				t.Fatalf("%v returned nil error; output:\n%s", args, out)
			}
			msg := err.Error()
			for _, want := range []string{
				`api "nope" not found`,
				"echo-test-api-2026",
				"echo-test-api",
				"musterflow list",
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("%v error missing %q:\n%s", args, want, msg)
				}
			}
			// DF-024: the registry error must reach the user unchanged — an
			// extra layer such as app.Refresh's "get api: " wrapper hides
			// neither ids nor names, but it also must not appear, because the
			// CLI resolved the argument itself.
			if strings.Contains(msg, "get api:") {
				t.Errorf("%v error was wrapped instead of surfaced unchanged:\n%s", args, msg)
			}
		})
	}
}

// TestUnknownArgOnEmptyRegistrySaysHowToConnect keeps the zero-connection case
// actionable instead of printing an empty "connected:" list.
func TestUnknownArgOnEmptyRegistrySaysHowToConnect(t *testing.T) {
	for _, args := range [][]string{{"refresh", "nope"}, {"catalog", "push", "nope"}} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			r := app.NewRegistry(t.TempDir())
			if err := r.Load(); err != nil {
				t.Fatalf("Load: %v", err)
			}
			t.Cleanup(func() { _ = r.Close() })
			SetStoreError(nil)
			SetDashboardURL("")
			t.Cleanup(func() { SetDashboardURL("") })

			out, err := runCLI(t, r, args...)
			if err == nil {
				t.Fatalf("%v returned nil error; output:\n%s", args, out)
			}
			msg := err.Error()
			for _, want := range []string{`api "nope" not found`, "no APIs are connected", "musterflow connect"} {
				if !strings.Contains(msg, want) {
					t.Errorf("%v error missing %q:\n%s", args, want, msg)
				}
			}
		})
	}
}
