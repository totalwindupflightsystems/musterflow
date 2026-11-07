package dashboard

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// metricsPrefix is the stable name prefix for every metric this server exports.
// The fleet monitor scrapes these by name, so the prefix must not change without
// a migration note in docs/operations.md.
const metricsPrefix = "musterflow"

// histogramBuckets are the upper bounds (seconds) of the request-duration
// histogram. They cover a dashboard HTTP call (milliseconds) through a flow run
// that talks to a remote API (seconds).
var histogramBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// jobRoutes are the routes that execute work on the caller's behalf (a flow run,
// a webhook trigger, an MCP tool call). Everything else is a read or a page.
var jobRoutes = map[string]bool{
	"/api/flows/": true,
	"/hooks/":     true,
	"/mcp":        true,
}

// staticRoutes are the exact-match routes registered on the mux. Prefix routes
// are listed separately so their sub-paths fold into the template.
var staticRoutes = map[string]bool{
	"/api/health":         true,
	"/api/apis":           true,
	"/api/flows":          true,
	"/api/catalog/search": true,
	"/api/mcp/info":       true,
	"/api/export":         true,
	"/api/import":         true,
	"/mcp":                true,
}

// prefixRoutes are the mux patterns ending in "/" whose sub-paths belong to the
// same route template. Longest first, so the walk is deterministic.
var prefixRoutes = []string{"/api/catalog/search", "/api/apis/", "/api/flows/", "/hooks/"}

// normalizeRoute maps a request path onto a bounded route template. An
// unbounded path label is a cardinality bomb: one caller looping over ids would
// otherwise mint a time series per id.
func normalizeRoute(p string) string {
	if staticRoutes[p] {
		return p
	}
	for _, prefix := range prefixRoutes {
		if strings.HasPrefix(p, prefix) {
			return prefix
		}
	}
	// The remaining paths are served by the "/" catch-all (the dashboard SPA).
	return "/"
}

// requestKey identifies one request counter series.
type requestKey struct {
	method string
	route  string
	code   string
}

// errorKey identifies one error-counter series. reason distinguishes failure
// causes so a red dashboard can be attributed without reading logs.
type errorKey struct {
	route  string
	reason string
}

// histogramState accumulates one route's duration observations.
type histogramState struct {
	buckets []uint64
	sum     float64
	count   uint64
}

// Metrics is a small, dependency-free Prometheus text-format exporter. It is
// deliberately hand-rolled: the daemon's only observability requirement is a
// scrapeable counter/histogram surface, and a prometheus client dependency for
// five metric families would be a heavier change than the gap warrants.
type Metrics struct {
	mu        sync.Mutex
	start     time.Time
	requests  map[requestKey]uint64
	jobs      map[requestKey]uint64
	errors    map[errorKey]uint64
	durations map[string]*histogramState
}

// NewMetrics returns an empty collector stamped with the process start time.
func NewMetrics() *Metrics {
	return &Metrics{
		start:     time.Now(),
		requests:  make(map[requestKey]uint64),
		jobs:      make(map[requestKey]uint64),
		errors:    make(map[errorKey]uint64),
		durations: make(map[string]*histogramState),
	}
}

// recorder captures the status code a handler wrote so the middleware can label
// it without the handler cooperating.
type recorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// Instrument wraps a handler with request counting, duration observation and
// error accounting. /metrics itself is never counted: a scrape loop would
// otherwise inflate the very numbers it reports.
func (m *Metrics) Instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		route := normalizeRoute(r.URL.Path)
		started := time.Now()
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		m.observe(route, r.Method, rec.status, time.Since(started))
	})
}

// observe records one completed request.
func (m *Metrics) observe(route, method string, status int, elapsed time.Duration) {
	code := strconv.Itoa(status)
	key := requestKey{method: method, route: route, code: code}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests[key]++
	if jobRoutes[route] && method != http.MethodGet && method != http.MethodHead {
		m.jobs[key]++
	}
	if status >= 400 {
		m.errors[errorKey{route: route, reason: errorReason(status)}]++
	}
	h := m.durations[route]
	if h == nil {
		h = &histogramState{buckets: make([]uint64, len(histogramBuckets))}
		m.durations[route] = h
	}
	secs := elapsed.Seconds()
	for i, le := range histogramBuckets {
		if secs <= le {
			h.buckets[i]++
		}
	}
	h.sum += secs
	h.count++
}

// errorReason maps a status code onto a bounded failure-cause label. The four
// classes are what an operator acts on: the client sent something wrong, the
// server failed, a dependency timed out, or the handler panicked (recorded by
// the recovery path, which reports 500).
func errorReason(status int) string {
	switch {
	case status >= 500:
		return "server_error"
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "auth_denied"
	default:
		return "client_error"
	}
}

// HandleMetrics renders the Prometheus text exposition format. Sections are
// emitted in a stable order so a diff of two scrapes shows only real changes.
func (m *Metrics) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder
	writeHelp(&b, metricsPrefix+"_build_info", "Build information for the running musterflow dashboard.", "gauge")
	fmt.Fprintf(&b, "%s_build_info{go_version=%q} 1\n", metricsPrefix, runtime.Version())

	writeHelp(&b, metricsPrefix+"_process_start_time_seconds", "Unix start time of the dashboard process.", "gauge")
	fmt.Fprintf(&b, "%s_process_start_time_seconds %d\n", metricsPrefix, m.start.Unix())

	writeHelp(&b, metricsPrefix+"_http_requests_total", "HTTP requests served, by method, route template and status code.", "counter")
	for _, k := range sortedRequestKeys(m.requests) {
		fmt.Fprintf(&b, "%s_http_requests_total{method=%q,path=%q,code=%q} %d\n",
			metricsPrefix, k.method, k.route, k.code, m.requests[k])
	}

	writeHelp(&b, metricsPrefix+"_jobs_total", "Jobs started through the server (flow runs, webhook triggers, MCP calls).", "counter")
	for _, k := range sortedRequestKeys(m.jobs) {
		fmt.Fprintf(&b, "%s_jobs_total{method=%q,path=%q,code=%q} %d\n",
			metricsPrefix, k.method, k.route, k.code, m.jobs[k])
	}

	writeHelp(&b, metricsPrefix+"_errors_total", "Failed requests, by route template and failure cause.", "counter")
	for _, k := range sortedErrorKeys(m.errors) {
		fmt.Fprintf(&b, "%s_errors_total{path=%q,reason=%q} %d\n", metricsPrefix, k.route, k.reason, m.errors[k])
	}

	writeHelp(&b, metricsPrefix+"_http_request_duration_seconds", "HTTP request latency, by route template.", "histogram")
	for _, route := range sortedRoutes(m.durations) {
		h := m.durations[route]
		for i, le := range histogramBuckets {
			fmt.Fprintf(&b, "%s_http_request_duration_seconds_bucket{path=%q,le=%q} %d\n",
				metricsPrefix, route, formatFloat(le), h.buckets[i])
		}
		fmt.Fprintf(&b, "%s_http_request_duration_seconds_bucket{path=%q,le=\"+Inf\"} %d\n",
			metricsPrefix, route, h.count)
		fmt.Fprintf(&b, "%s_http_request_duration_seconds_sum{path=%q} %s\n",
			metricsPrefix, route, formatSum(h.sum))
		fmt.Fprintf(&b, "%s_http_request_duration_seconds_count{path=%q} %d\n",
			metricsPrefix, route, h.count)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

func writeHelp(b *strings.Builder, name, help, kind string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s %s\n", name, kind)
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func formatSum(f float64) string {
	return strconv.FormatFloat(f, 'f', 6, 64)
}

func sortedRequestKeys(m map[requestKey]uint64) []requestKey {
	keys := make([]requestKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		if keys[i].method != keys[j].method {
			return keys[i].method < keys[j].method
		}
		return keys[i].code < keys[j].code
	})
	return keys
}

func sortedErrorKeys(m map[errorKey]uint64) []errorKey {
	keys := make([]errorKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].reason < keys[j].reason
	})
	return keys
}

func sortedRoutes(m map[string]*histogramState) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
