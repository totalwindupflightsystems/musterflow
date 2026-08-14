package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestParseDataDirFlag verifies the --data-dir flag is parsed from raw CLI
// args in BOTH "--data-dir <path>" and "--data-dir=<path>" forms. This is
// the BUG-004 regression test: cobra parses persistent flags only during
// ExecuteContext (after the registry is built), so the flag must be read
// manually before registry construction.
func TestParseDataDirFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"space form", []string{"flow", "create", "x", "--data-dir", "/tmp/mf-dir"}, "/tmp/mf-dir"},
		{"equals form", []string{"--data-dir=/tmp/mf-eq", "list"}, "/tmp/mf-eq"},
		{"flag first", []string{"--data-dir", "/tmp/mf-first", "start"}, "/tmp/mf-first"},
		{"absent", []string{"flow", "list"}, ""},
		{"empty value", []string{"--data-dir=", "list"}, ""},
		{"not data-dir", []string{"--dashboard-addr", ":9999", "start"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDataDirFlag(tt.args); got != tt.want {
				t.Errorf("parseDataDirFlag(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// TestParseDashboardAddrFlag verifies the --dashboard-addr flag is parsed
// from raw CLI args in both "--dashboard-addr <value>" and
// "--dashboard-addr=<value>" forms. See DF-013.
func TestParseDashboardAddrFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"space form", []string{"--dashboard-addr", "127.0.0.1:9876", "connect", "url"}, "127.0.0.1:9876"},
		{"equals form", []string{"--dashboard-addr=127.0.0.1:9876", "list"}, "127.0.0.1:9876"},
		{"absent", []string{"connect", "http://example.com/spec.json"}, ""},
		{"empty value", []string{"--dashboard-addr=", "list"}, ""},
		{"other flag", []string{"--data-dir", "/tmp/x", "start"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDashboardAddrFlag(tt.args); got != tt.want {
				t.Errorf("parseDashboardAddrFlag(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// TestParseNoDashboardFlag verifies the --no-dashboard flag is detected from
// raw CLI args. See DF-013.
func TestParseNoDashboardFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"bare flag", []string{"--no-dashboard", "connect", "url"}, true},
		{"equals true", []string{"--no-dashboard=true", "list"}, true},
		{"equals 1", []string{"--no-dashboard=1", "connect", "url"}, true},
		{"equals false", []string{"--no-dashboard=false", "list"}, false},
		{"absent", []string{"connect", "url"}, false},
		{"other flag only", []string{"--data-dir", "/tmp/x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseNoDashboardFlag(tt.args); got != tt.want {
				t.Errorf("parseNoDashboardFlag(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

// TestIsMusterflowDashboard_RealDashboard verifies that a real musterflow-shaped
// health endpoint (JSON {"status":"ok"}) causes isMusterflowDashboard to return
// true — the dashboard-running path where CLI routes through the dashboard API.
func TestIsMusterflowDashboard_RealDashboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":         "ok",
				"connected_apis": 3,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if !isMusterflowDashboard(addr) {
		t.Error("expected isMusterflowDashboard=true for a real musterflow health endpoint")
	}
}

// TestIsMusterflowDashboard_ForeignServer verifies that a foreign HTTP server
// (e.g. python3 -m http.server) that answers on the port but does NOT return
// musterflow-shaped JSON is treated as "no dashboard". This is the core DF-013
// fix: a foreign process squatting on the configured port must not cause
// musterflow to route through it as if it were the dashboard.
func TestIsMusterflowDashboard_ForeignServer(t *testing.T) {
	// python3 -m http.server returns a directory listing (HTML, 200),
	// not JSON with {"status":"ok"}.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Index of /</body></html>"))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if isMusterflowDashboard(addr) {
		t.Error("expected isMusterflowDashboard=false for a foreign HTTP server")
	}
}

// TestIsMusterflowDashboard_ForeignNonHTTP verifies that a bare TCP listener
// (e.g. a dagger engine) that accepts connections but does not speak HTTP
// is treated as "no dashboard".
func TestIsMusterflowDashboard_ForeignNonHTTP(t *testing.T) {
	// Start a raw TCP listener that accepts connections but does not
	// respond with HTTP.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind for test: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Write a raw non-HTTP response (e.g. a number).
			_, _ = conn.Write([]byte("42"))
			_ = conn.Close()
		}
	}()

	addr := ln.Addr().String()
	if isMusterflowDashboard(addr) {
		t.Error("expected isMusterflowDashboard=false for a non-HTTP TCP listener")
	}
}

// TestIsMusterflowDashboard_FreePort verifies that an address with nothing
// listening returns false — the dashboard-NOT-running path.
func TestIsMusterflowDashboard_FreePort(t *testing.T) {
	// Pick a random free port by opening and immediately closing a listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	if isMusterflowDashboard(addr) {
		t.Error("expected isMusterflowDashboard=false for a free port")
	}
}

// TestIsMusterflowDashboard_WrongShapeJSON verifies that a server returning
// 200 with JSON that does NOT have {"status":"ok"} is rejected.
func TestIsMusterflowDashboard_WrongShapeJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Wrong shape — no "status" field.
		_, _ = w.Write([]byte(`{"foo":"bar","count":42}`))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if isMusterflowDashboard(addr) {
		t.Error("expected isMusterflowDashboard=false for wrong-shape JSON")
	}
}

// TestIsMusterflowDashboard_404 verifies that a 404 on /api/health is
// treated as "no dashboard" (even if the server speaks HTTP and returns JSON).
func TestIsMusterflowDashboard_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if isMusterflowDashboard(addr) {
		t.Error("expected isMusterflowDashboard=false for HTTP 404")
	}
}

// TestIsMusterflowDashboard_Timeout verifies the 2s timeout does not hang
// when the address is unreachable.
func TestIsMusterflowDashboard_Timeout(t *testing.T) {
	// 192.0.2.1 is TEST-NET-1 (RFC 5737) — guaranteed to be unroutable.
	start := time.Now()
	_ = isMusterflowDashboard("192.0.2.1:9999")
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Errorf("isMusterflowDashboard took %v, expected <5s (timeout should be 2s)", elapsed)
	}
}
