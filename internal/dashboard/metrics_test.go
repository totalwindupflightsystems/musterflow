package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// newMetricsTestServer builds a dashboard server over a temp registry and
// returns it with its instrumented handler.
func newMetricsTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := NewServer(r, nil, nil, ":0")
	return s, s.Handler()
}

// scrape performs one GET /metrics and returns its body.
func scrape(t *testing.T, h http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics: expected 200, got %d", rec.Code)
	}
	return rec.Body.String()
}

// hit issues one request against the handler and returns the status code.
func hit(t *testing.T, h http.Handler, method, path string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestMetrics_EndpointServesPrometheusText(t *testing.T) {
	_, h := newMetricsTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") || !strings.Contains(ct, "version=0.0.4") {
		t.Fatalf("expected prometheus text content type, got %q", ct)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE musterflow_http_requests_total counter",
		"# TYPE musterflow_jobs_total counter",
		"# TYPE musterflow_errors_total counter",
		"# TYPE musterflow_http_request_duration_seconds histogram",
		"musterflow_build_info{",
		"musterflow_process_start_time_seconds ",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape is missing %q", want)
		}
	}
}

func TestMetrics_CountsRequestsByRoute(t *testing.T) {
	_, h := newMetricsTestServer(t)

	hit(t, h, http.MethodGet, "/api/health")
	hit(t, h, http.MethodGet, "/api/health")

	body := scrape(t, h)
	want := `musterflow_http_requests_total{method="GET",path="/api/health",code="200"} 2`
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q in scrape, got:\n%s", want, body)
	}
}

func TestMetrics_SelfScrapeExcluded(t *testing.T) {
	_, h := newMetricsTestServer(t)

	for i := 0; i < 3; i++ {
		scrape(t, h)
	}

	body := scrape(t, h)
	if strings.Contains(body, `path="/metrics"`) {
		t.Fatalf("self-scrape must not be counted, scrape contains a /metrics series:\n%s", body)
	}
}

func TestMetrics_ErrorCounterDistinguishesCause(t *testing.T) {
	_, h := newMetricsTestServer(t)

	// A PUT on /api/apis is method-not-allowed: a 4xx client error.
	code := hit(t, h, http.MethodPut, "/api/apis")
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 from PUT /api/apis, got %d", code)
	}

	body := scrape(t, h)
	want := `musterflow_errors_total{path="/api/apis",reason="client_error"} 1`
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q in scrape, got:\n%s", want, body)
	}
	if strings.Contains(body, `reason="server_error"`) {
		t.Errorf("a 405 must not be classified as server_error")
	}
}

func TestMetrics_HistogramBucketsTrackRequests(t *testing.T) {
	_, h := newMetricsTestServer(t)

	const n = 3
	for i := 0; i < n; i++ {
		hit(t, h, http.MethodGet, "/api/health")
	}

	body := scrape(t, h)
	if !strings.Contains(body, `musterflow_http_request_duration_seconds_bucket{path="/api/health",le="+Inf"} 3`) {
		t.Fatalf("expected +Inf bucket at 3 for /api/health, got:\n%s", body)
	}
	countLine := `musterflow_http_request_duration_seconds_count{path="/api/health"} `
	idx := strings.Index(body, countLine)
	if idx < 0 {
		t.Fatalf("missing %q", countLine)
	}
	rest := body[idx+len(countLine):]
	end := strings.IndexByte(rest, '\n')
	if end < 0 {
		end = len(rest)
	}
	got, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		t.Fatalf("parse count: %v", err)
	}
	if got != n {
		t.Errorf("expected count %d, got %d", n, got)
	}
}

func TestMetrics_PathLabelsAreBounded(t *testing.T) {
	_, h := newMetricsTestServer(t)

	// Arbitrary ids must fold onto the route template, not mint a series each.
	hit(t, h, http.MethodGet, "/api/apis/abc123")
	hit(t, h, http.MethodGet, "/api/apis/def456")
	hit(t, h, http.MethodGet, "/definitely-not-a-route")

	body := scrape(t, h)
	if strings.Contains(body, "abc123") || strings.Contains(body, "def456") {
		t.Fatalf("raw path segment leaked into a label:\n%s", body)
	}
	if !strings.Contains(body, `path="/api/apis/"`) {
		t.Errorf("expected requests to fold onto /api/apis/")
	}
	if !strings.Contains(body, `path="/"`) {
		t.Errorf("expected an unmatched path to fold onto the catch-all route")
	}
}

func TestMetrics_JobCounterCountsNonGETJobRoutes(t *testing.T) {
	_, h := newMetricsTestServer(t)

	hit(t, h, http.MethodPost, "/api/flows/nope/run")

	body := scrape(t, h)
	if !strings.Contains(body, `musterflow_jobs_total{method="POST",path="/api/flows/"`) {
		t.Fatalf("expected a job counter entry for POST /api/flows/, got:\n%s", body)
	}
	// A GET on the same route is a read, not a job.
	hit(t, h, http.MethodGet, "/api/flows")
	body = scrape(t, h)
	if strings.Contains(body, `musterflow_jobs_total{method="GET"`) {
		t.Errorf("GET must not be counted as a job")
	}
}

func TestNormalizeRoute(t *testing.T) {
	cases := map[string]string{
		"/api/health":                "/api/health",
		"/api/apis":                  "/api/apis",
		"/api/apis/abc":              "/api/apis/",
		"/api/flows/foo/run":         "/api/flows/",
		"/api/catalog/search?q=x":    "/api/catalog/search",
		"/hooks/github":              "/hooks/",
		"/index.html":                "/",
		"/":                          "/",
		"/something/entirely-random": "/",
	}
	for in, want := range cases {
		if got := normalizeRoute(in); got != want {
			t.Errorf("normalizeRoute(%q) = %q, want %q", in, got, want)
		}
	}
}
