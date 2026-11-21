package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A dashboard started with `dashboard_token` answers 401 to every routed CLI
// call that does not present it, so the CLI must attach the bearer token — to
// the dashboard origin only. The same HTTP client fetches third-party OpenAPI
// specs, which must never receive it.
func TestDashboardTokenScopedToDashboardOrigin(t *testing.T) {
	const token = "tok-1234567890"

	var dashboardAuth string
	dashboard := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dashboardAuth = r.Header.Get("Authorization")
		if dashboardAuth != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"apis":[]}`)
	}))
	defer dashboard.Close()

	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			t.Errorf("dashboard token leaked to a foreign origin: %q", h)
		}
		_, _ = io.WriteString(w, "openapi: 3.0.0\n")
	}))
	defer foreign.Close()

	oldURL, oldToken := dashboardBaseURL, dashboardToken
	t.Cleanup(func() {
		dashboardBaseURL = oldURL
		dashboardToken = oldToken
	})

	SetDashboardToken(token)
	SetDashboardURL(dashboard.URL)

	if _, err := fetchAPIsFromDashboard(); err != nil {
		t.Fatalf("fetchAPIsFromDashboard against an authenticated dashboard: %v", err)
	}
	if dashboardAuth != "Bearer "+token {
		t.Fatalf("dashboard saw Authorization %q, want the bearer token", dashboardAuth)
	}

	resp, err := http.Get(foreign.URL + "/openapi.yaml")
	if err != nil {
		t.Fatalf("foreign fetch: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("foreign fetch = %d", resp.StatusCode)
	}
}

func TestSameOrigin(t *testing.T) {
	cases := []struct {
		target, base string
		want         bool
	}{
		{"http://127.0.0.1:9876/api/apis", "http://127.0.0.1:9876", true},
		{"http://127.0.0.1:9877/api/apis", "http://127.0.0.1:9876", false},
		{"http://127.0.0.1.example.com:9876/api/apis", "http://127.0.0.1:9876", false},
		{"https://127.0.0.1:9876/api/apis", "http://127.0.0.1:9876", false},
		{"http://example.com/spec.yaml", "http://127.0.0.1:9876", false},
	}
	for _, tc := range cases {
		req, err := http.NewRequest(http.MethodGet, tc.target, nil)
		if err != nil {
			t.Fatalf("NewRequest(%q): %v", tc.target, err)
		}
		if got := sameOrigin(req.URL, tc.base); got != tc.want {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v", tc.target, tc.base, got, tc.want)
		}
	}
}
