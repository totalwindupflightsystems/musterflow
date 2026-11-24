package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
	"github.com/totalwindupflightsystems/musterflow/internal/dashboard"
)

// DF-052: a CLI pointed at a tokened dashboard it cannot authenticate to must
// fail naming the authentication cause and the token variable — both for
// `list` and for generated API subcommands. The pre-fix outcomes — the
// registry-fallback warning, an empty registry, and cobra's
// `unknown command "<api>" for "musterflow"` — must not survive.
func TestDashboardAuthFailureNamesTokenNotRegistryOrCommandTree(t *testing.T) {
	const token = "tok-0123456789abcdef"
	home := t.TempDir()
	defer setHome(t, home)()

	// One-operation spec server so the dashboard really holds a connection;
	// the user "should" be able to call its generated commands.
	specJSON := `{"openapi":"3.0.3","info":{"title":"Pet Store","version":"1.0.0"},"servers":[{"url":"http://localhost:9999"}],"paths":{"/pets":{"get":{"operationId":"listPets","summary":"List pets","responses":{"200":{"description":"ok"}}}}}}`
	specServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(specJSON))
	}))
	defer specServer.Close()

	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = r.Close() }()

	result, err := app.Connect(context.Background(), r, app.ConnectOptions{
		SpecURL: specServer.URL,
		Name:    "petstore",
	})
	if err != nil {
		t.Fatalf("Connect (seeding the dashboard's registry): %v", err)
	}
	connName := result.Connection.Name

	// The dashboard is started WITH a token; the CLI presents none.
	dash, err := dashboard.NewServerWithToken(r, catalog.NewClient(), nil, "127.0.0.1:0", token)
	if err != nil {
		t.Fatalf("NewServerWithToken: %v", err)
	}
	ts := httptest.NewServer(dash.Handler())
	defer ts.Close()

	oldURL, oldToken := dashboardBaseURL, dashboardToken
	t.Cleanup(func() {
		dashboardBaseURL = oldURL
		dashboardToken = oldToken
		SetDashboardAuthError(nil)
	})
	dashboardToken = ""
	SetDashboardURL(ts.URL)

	t.Run("list", func(t *testing.T) {
		root := NewRootCommand(r)
		stderr := new(strings.Builder)
		root.SetErr(stderr)
		root.SetArgs([]string{"list"})
		var execErr error
		output := captureStdout(func() {
			execErr = ResolveExecutionError(root.Execute())
		})
		if execErr == nil {
			t.Fatal("expected `list` to fail against a tokened dashboard the CLI cannot authenticate to")
		}
		msg := execErr.Error()
		for _, want := range []string{"dashboard authentication failed", "HTTP 401", "MUSTERFLOW_DASHBOARD_TOKEN", "dashboard_token"} {
			if !strings.Contains(msg, want) {
				t.Errorf("`list` error does not name %q: %q", want, msg)
			}
		}
		for _, masked := range []string{"dashboard returned HTTP", "unknown command"} {
			if strings.Contains(msg, masked) {
				t.Errorf("`list` error must not use the pre-fix %q wording: %q", masked, msg)
			}
		}
		combined := output + stderr.String()
		if strings.Contains(combined, "falling back to local registry") {
			t.Errorf("auth failure masked by the local-registry fallback warning:\n%s", combined)
		}
		if strings.Contains(combined, "No APIs connected.") {
			t.Errorf("auth failure masked as an empty registry:\n%s", combined)
		}
	})

	t.Run("generated API subcommand", func(t *testing.T) {
		root := NewRootCommand(r)
		root.SetArgs([]string{connName, "listPets"})
		var rawErr error
		captureStdout(func() {
			rawErr = root.Execute()
		})
		// The tree build was refused by the dashboard, so cobra only knows
		// the symptom. Assert the premise (the unknown-command shape the
		// brief observed) before asserting the resolution.
		if rawErr == nil {
			t.Fatalf("expected `musterflow %s listPets` to fail", connName)
		}
		if !strings.HasPrefix(rawErr.Error(), "unknown command ") || !strings.Contains(rawErr.Error(), `"`+connName+`"`) {
			t.Fatalf("premise drift: expected cobra's unknown-command symptom, got %q", rawErr)
		}
		resolved := ResolveExecutionError(rawErr)
		msg := resolved.Error()
		for _, want := range []string{"dashboard authentication failed", "HTTP 401", "MUSTERFLOW_DASHBOARD_TOKEN", "dashboard_token"} {
			if !strings.Contains(msg, want) {
				t.Errorf("resolved error does not name %q: %q", want, msg)
			}
		}
		if strings.Contains(msg, "unknown command") {
			t.Errorf("unknown-command outcome survives the auth failure: %q", msg)
		}
	})
}

// The auth classification is bounded to 401/403: every other dashboard status
// keeps the exact pre-fix wording, and a non-empty relayed body (DF-013/DF-035)
// is still relayed verbatim. Connection errors and timeouts never pass through
// these helpers at all (transport failures return before the status check).
func TestDashboardStatusErrorKeepsHistoricalWordingForNonAuth(t *testing.T) {
	t.Cleanup(func() { SetDashboardAuthError(nil) })

	cases := []struct {
		name string
		got  string
		want string
	}{
		// bare-status sites: 404/500 keep the historical wording
		{"bare 404", dashboardStatusError(http.StatusNotFound).Error(), "dashboard returned HTTP 404"},
		// action sites: relayed body attached verbatim, or bare status
		{"relay 409", dashboardActionError("connect via dashboard", http.StatusConflict, "spec version already connected").Error(), "connect via dashboard: spec version already connected"},
		{"bare 500", dashboardActionError("refresh via dashboard", http.StatusInternalServerError, "").Error(), "refresh via dashboard: dashboard returned HTTP 500"},
		// 401/403 rename; the auth message replaces the action wrapping and
		// the relayed body is preserved verbatim
		{"401 bare", dashboardStatusError(http.StatusUnauthorized).Error(), "dashboard authentication failed (HTTP 401)"},
		{"403 relay", dashboardActionError("refresh via dashboard", http.StatusForbidden, "token expired").Error(), "(dashboard said: token expired)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.got, tc.want) {
				t.Errorf("error = %q, want it to contain %q", tc.got, tc.want)
			}
		})
	}
	for _, want := range []string{"MUSTERFLOW_DASHBOARD_TOKEN", "dashboard_token"} {
		if got := dashboardStatusError(http.StatusUnauthorized).Error(); !strings.Contains(got, want) {
			t.Errorf("auth error missing %q: %q", want, got)
		}
	}
}
