package dashboard

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
	"github.com/totalwindupflightsystems/musterflow/internal/workflow"
)

// --- Address construction (the "--dashboard-addr host is discarded" defect) ---
//
// cmd/musterflow/startServer used to parse only the PORT out of the flag and
// then build ":<port>", which net.Listen reads as "every interface". ListenAddr
// is the single place that turns the flag into a listen address, so these
// cases pin the rule that a host-less address can never come back out.

func TestListenAddr(t *testing.T) {
	cases := []struct {
		name        string
		requested   string
		defaultPort int
		want        string
		wantErr     bool
	}{
		{name: "flag absent uses config port on loopback", requested: "", defaultPort: 9876, want: "127.0.0.1:9876"},
		{name: "host-less port becomes loopback", requested: ":19941", defaultPort: 9876, want: "127.0.0.1:19941"},
		{name: "explicit loopback host is preserved", requested: "127.0.0.1:19941", defaultPort: 9876, want: "127.0.0.1:19941"},
		{name: "localhost is preserved", requested: "localhost:19941", defaultPort: 9876, want: "localhost:19941"},
		{name: "ipv6 loopback is preserved", requested: "[::1]:19941", defaultPort: 9876, want: "[::1]:19941"},
		{name: "wildcard v4 is preserved (refused later without a token)", requested: "0.0.0.0:19941", defaultPort: 9876, want: "0.0.0.0:19941"},
		{name: "wildcard v6 is preserved (refused later without a token)", requested: "[::]:19941", defaultPort: 9876, want: "[::]:19941"},
		{name: "specific external ip is preserved", requested: "192.0.2.10:19941", defaultPort: 9876, want: "192.0.2.10:19941"},
		{name: "ephemeral port is allowed", requested: ":0", defaultPort: 9876, want: "127.0.0.1:0"},
		{name: "missing port is refused", requested: "127.0.0.1", wantErr: true},
		{name: "non-numeric port is refused", requested: ":abc", wantErr: true},
		{name: "out of range port is refused", requested: ":70000", wantErr: true},
		{name: "negative port is refused", requested: ":-1", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ListenAddr(tc.requested, tc.defaultPort)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ListenAddr(%q) = %q, want error", tc.requested, got)
				}
				if got != "" {
					t.Fatalf("ListenAddr(%q) returned %q with an error, want empty", tc.requested, got)
				}
				// The existing --dashboard-addr usage hint must survive.
				if !strings.Contains(err.Error(), "--dashboard-addr") {
					t.Errorf("error %q does not name --dashboard-addr", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ListenAddr(%q) error: %v", tc.requested, err)
			}
			if got != tc.want {
				t.Fatalf("ListenAddr(%q) = %q, want %q", tc.requested, got, tc.want)
			}
			// The defect this guards: a host-less address binds every interface.
			if strings.HasPrefix(got, ":") {
				t.Fatalf("ListenAddr(%q) = %q: host-less addresses are never allowed", tc.requested, got)
			}
		})
	}
}

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:9876", true},
		{"127.0.0.53:9876", true},
		{"localhost:9876", true},
		{"LOCALHOST:9876", true},
		{"[::1]:9876", true},
		{"0.0.0.0:9876", false},
		{"[::]:9876", false},
		{"192.0.2.10:9876", false},
		{"100.99.183.108:9876", false},
		{"example.com:9876", false},
		{":9876", false},
		{"", false},
		{"not-an-address", false},
	}
	for _, tc := range cases {
		if got := IsLoopbackAddr(tc.addr); got != tc.want {
			t.Errorf("IsLoopbackAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestBaseURL(t *testing.T) {
	cases := []struct{ addr, want string }{
		{"127.0.0.1:9876", "http://localhost:9876"},
		{"localhost:9876", "http://localhost:9876"},
		{"[::1]:9876", "http://localhost:9876"},
		{":9876", "http://localhost:9876"},
		{"0.0.0.0:9876", "http://0.0.0.0:9876"},
		{"192.0.2.10:9876", "http://192.0.2.10:9876"},
	}
	for _, tc := range cases {
		if got := BaseURL(tc.addr); got != tc.want {
			t.Errorf("BaseURL(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

// --- Fail-closed exposure policy ---

func authTestRegistry(t *testing.T) *app.Registry {
	t.Helper()
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load registry: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func authTestServer(t *testing.T, token string) *Server {
	t.Helper()
	return authTestServerAt(t, "127.0.0.1:0", token)
}

func authTestServerAt(t *testing.T, addr, token string) *Server {
	t.Helper()
	s, err := NewServerWithToken(authTestRegistry(t), catalog.NewClient(), nil, addr, token)
	if err != nil {
		t.Fatalf("NewServerWithToken(%q, token=%v): %v", addr, token != "", err)
	}
	return s
}

func TestNewServerWithToken_RefusesExposedBindWithoutToken(t *testing.T) {
	r := authTestRegistry(t)

	for _, addr := range []string{"0.0.0.0:19941", "[::]:19941", "192.0.2.10:19941"} {
		t.Run(addr, func(t *testing.T) {
			s, err := NewServerWithToken(r, nil, nil, addr, "")
			if err == nil {
				t.Fatalf("NewServerWithToken(%q) with no token returned a server, want refusal", addr)
			}
			if s != nil {
				t.Fatalf("NewServerWithToken(%q) returned a server alongside the error", addr)
			}
			// The refusal has to be actionable: both ways out are named.
			for _, want := range []string{"dashboard_token", "loopback"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal for %q does not mention %q: %v", addr, want, err)
				}
			}

			// Same address WITH a token is allowed — exposure is a policy,
			// not a blanket ban on non-loopback binds.
			if _, err := NewServerWithToken(r, nil, nil, addr, "s3cr3t-token"); err != nil {
				t.Fatalf("NewServerWithToken(%q) with a token: %v", addr, err)
			}
		})
	}
}

func TestNewServerWithToken_AllowsLoopbackWithoutToken(t *testing.T) {
	// The sibling cross-task contract: a loopback fresh start with no token
	// configured serves the API openly (200, not 401).
	for _, addr := range []string{"127.0.0.1:0", ":0", "localhost:0", "[::1]:0"} {
		t.Run(addr, func(t *testing.T) {
			s := authTestServerAt(t, addr, "")
			if s.AuthEnabled() {
				t.Fatalf("AuthEnabled() = true for a token-less loopback server")
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/flows", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /api/flows on token-less loopback = %d, want 200: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// --- Bearer auth on /api/*, /mcp, /hooks/* ---

func TestProtectedRoutesRequireBearerToken(t *testing.T) {
	const token = "s3cr3t-dashboard-token-0123456789"

	protected := []struct{ method, path string }{
		{http.MethodGet, "/api/apis"},
		{http.MethodGet, "/api/flows"},
		{http.MethodGet, "/api/export"},
		{http.MethodGet, "/api/mcp/info"},
		{http.MethodGet, "/api/catalog/search?q=example"},
		{http.MethodPost, "/mcp"},
		{http.MethodPost, "/hooks/no-such-flow"},
	}
	// What a 401 must never echo back.
	leakMarkers := []string{"flows", "apis", "jsonrpc", "spec_url", "print("}

	for _, tc := range protected {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			s := authTestServer(t, token)

			deny := func(name string, header string) {
				t.Helper()
				req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
				if header != "" {
					req.Header.Set("Authorization", header)
				}
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, req)
				if rec.Code != http.StatusUnauthorized {
					t.Fatalf("%s: %s %s = %d, want 401: %s", name, tc.method, tc.path, rec.Code, rec.Body.String())
				}
				body := strings.TrimSpace(rec.Body.String())
				if body != `{"error":"unauthorized"}` {
					t.Errorf("%s: 401 body = %q, want the fixed {\"error\":\"unauthorized\"}", name, body)
				}
				for _, marker := range leakMarkers {
					if strings.Contains(body, marker) {
						t.Errorf("%s: 401 body leaks %q: %s", name, marker, body)
					}
				}
			}

			deny("no token", "")
			deny("wrong token", "Bearer not-the-token")
			deny("wrong scheme", "Token "+token)
			deny("bare token without scheme", token)
			deny("token with wrong length", "Bearer s3cr3t-dashboard-token")

			// Correct token reaches the real handler. /hooks/no-such-flow
			// answers 404 (unknown flow), which is the point: auth passed.
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("correct token rejected on %s %s: %s", tc.method, tc.path, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "unauthorized") {
				t.Fatalf("correct token answered an auth error on %s %s: %s", tc.method, tc.path, rec.Body.String())
			}
		})
	}
}

func TestProtectedRouteCorrectTokenServesData(t *testing.T) {
	const token = "s3cr3t-dashboard-token-0123456789"
	s := authTestServer(t, token)

	// Seed a flow so the 200 path has real data to return.
	eng := workflow.NewEngine(s.flowsDir, s.baseURL())
	if _, err := eng.Create("authflow", `print("visible-only-with-token")`, "auth test flow", false); err != nil {
		t.Fatalf("create flow: %v", err)
	}

	get := func(header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/flows", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	if rec := get(""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: code = %d, want 401", rec.Code)
	} else if strings.Contains(rec.Body.String(), "authflow") {
		t.Fatalf("401 leaked the flow name: %s", rec.Body.String())
	}

	rec := get("Bearer " + token)
	if rec.Code != http.StatusOK {
		t.Fatalf("with token: code = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "authflow") {
		t.Fatalf("with token: body does not list the flow: %s", rec.Body.String())
	}
}

func TestHealthAndMetricsStayOpenWithToken(t *testing.T) {
	s := authTestServer(t, "s3cr3t-dashboard-token-0123456789")

	for _, path := range []string{"/api/health", "/metrics"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s without a token = %d, want 200 (deliberately unauthenticated)", path, rec.Code)
		}
	}
}

// --- /hooks/<flow> is POST-only ---

func TestWebhookIsPostOnly(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// No token: the method gate has to hold on its own, not as a side effect
	// of auth.
	s := authTestServerAt(t, "127.0.0.1:0", "")
	eng := workflow.NewEngine(s.flowsDir, s.baseURL())
	src := fmt.Sprintf("resp = http_get(%q)\nprint(str(resp[\"status\"]))", upstream.URL)
	if _, err := eng.Create("sideeffect", src, "side-effect probe", true); err != nil {
		t.Fatalf("create flow: %v", err)
	}

	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(method, "/hooks/sideeffect", nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s /hooks/sideeffect = %d, want 405: %s", method, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Allow"); got != http.MethodPost {
				t.Errorf("%s /hooks/sideeffect Allow = %q, want %q", method, got, http.MethodPost)
			}
			if n := atomic.LoadInt32(&hits); n != 0 {
				t.Fatalf("%s /hooks/sideeffect executed the flow: upstream hit %d times", method, n)
			}
		})
	}

	// Control: the same flow DOES run on POST, so the 405s above prove the
	// gate rather than a broken flow.
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hooks/sideeffect", strings.NewReader("{}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /hooks/sideeffect = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("POST /hooks/sideeffect upstream hits = %d, want 1", n)
	}
}

func TestWebhookEmptyNameStill400(t *testing.T) {
	s := authTestServerAt(t, "127.0.0.1:0", "")
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(method, "/hooks/", nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s /hooks/ = %d, want 400", method, rec.Code)
		}
	}
}

// TestWebhookRequiresTokenBeforeRunning proves auth is checked BEFORE the
// flow can run: with a token configured, an unauthenticated POST must not
// reach the engine.
func TestWebhookRequiresTokenBeforeRunning(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer upstream.Close()

	const token = "s3cr3t-dashboard-token-0123456789"
	s := authTestServer(t, token)
	eng := workflow.NewEngine(s.flowsDir, s.baseURL())
	src := fmt.Sprintf("resp = http_get(%q)\nprint(str(resp[\"status\"]))", upstream.URL)
	if _, err := eng.Create("guarded", src, "guarded probe", true); err != nil {
		t.Fatalf("create flow: %v", err)
	}

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/hooks/guarded", strings.NewReader("{}")))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /hooks/guarded without a token = %d, want 401", rec.Code)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("unauthenticated webhook ran the flow: upstream hit %d times", n)
	}

	req := httptest.NewRequest(http.MethodPost, "/hooks/guarded", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authorized POST /hooks/guarded = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("authorized webhook upstream hits = %d, want 1", n)
	}
}
