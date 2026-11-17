package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// freePort returns a port that is free on 127.0.0.1 at call time. Tests never
// use the real :9876 — a foreign process (a dagger engine, say) may hold it —
// so every case starts from an OS-assigned port.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind for test: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Skipf("cannot release test listener: %v", err)
	}
	return port
}

// musterflowHealthServer serves a musterflow-shaped health endpoint so the
// real dashboard probe (health + JSON shape, DF-013) recognises it.
func musterflowHealthServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSelectStartPort_FreeBase verifies the requested port is used as-is when
// nothing holds it, with no fallback note.
func TestSelectStartPort_FreeBase(t *testing.T) {
	base := freePort(t)

	choice, err := selectStartPort(base, dashboardPortOccupied)
	if err != nil {
		t.Fatalf("selectStartPort(%d): %v", base, err)
	}
	if choice.Port != base {
		t.Errorf("expected port %d, got %d", base, choice.Port)
	}
	if choice.Note != "" {
		t.Errorf("expected no fallback note for a free port, got %q", choice.Note)
	}
}

// TestSelectStartPort_ForeignOccupantFallsBack verifies the documented
// behaviour: a foreign process on the configured port is survived by serving
// on the next free port in the range, and the choice is reported.
func TestSelectStartPort_ForeignOccupantFallsBack(t *testing.T) {
	// A foreign HTTP server (the shape of `python3 -m http.server`) on the
	// base port: answers, but is not a MusterFlow dashboard.
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Index of /</body></html>"))
	}))
	defer foreign.Close()
	base := foreign.Listener.Addr().(*net.TCPAddr).Port

	// Occupy base+1 too, so the walk has to skip more than one port.
	next, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base+1))
	if err != nil {
		t.Skipf("cannot bind base+1 for test: %v", err)
	}
	defer func() { _ = next.Close() }()

	choice, err := selectStartPort(base, dashboardPortOccupied)
	if err != nil {
		t.Fatalf("selectStartPort(%d): %v", base, err)
	}
	if choice.Port < base+2 || choice.Port > base+9 {
		t.Errorf("expected a port in [%d, %d], got %d", base+2, base+9, choice.Port)
	}

	wantNote := fmt.Sprintf("port %d busy (foreign process) — serving on %d", base, choice.Port)
	if choice.Note != wantNote {
		t.Errorf("note = %q, want %q", choice.Note, wantNote)
	}

	// The chosen port must be bindable right now.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", choice.Port))
	if err != nil {
		t.Errorf("chosen port %d is not bindable: %v", choice.Port, err)
	} else {
		_ = ln.Close()
	}
}

// TestSelectStartPort_AllBusy verifies the whole range being taken fails with
// an actionable error naming the range and the override flag.
func TestSelectStartPort_AllBusy(t *testing.T) {
	// Reserve ten consecutive ports. If the OS hands out a base whose range
	// another process grabs first, retry with a different base.
	var held []net.Listener
	base := 0
	for attempt := 0; attempt < 20 && len(held) == 0; attempt++ {
		candidate := freePort(t)
		if candidate+9 > 65535 {
			continue
		}
		var reserved []net.Listener
		ok := true
		for offset := 0; offset < 10; offset++ {
			ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", candidate+offset))
			if err != nil {
				ok = false
				break
			}
			reserved = append(reserved, ln)
		}
		if ok {
			held, base = reserved, candidate
			continue
		}
		for _, ln := range reserved {
			_ = ln.Close()
		}
	}
	if len(held) == 0 {
		t.Skip("could not reserve ten consecutive ports")
	}
	defer func() {
		for _, ln := range held {
			_ = ln.Close()
		}
	}()

	choice, err := selectStartPort(base, func(int) bool { return false })
	if err == nil {
		t.Fatalf("expected an error when ports %d-%d are all busy, got port %d", base, base+9, choice.Port)
	}
	if choice.Port != 0 {
		t.Errorf("expected no chosen port on failure, got %d", choice.Port)
	}
	if want := fmt.Sprintf("no available port in range %d-%d", base, base+9); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the range (%q)", err, want)
	}
	if !strings.Contains(err.Error(), "--dashboard-addr") {
		t.Errorf("error %q does not point at --dashboard-addr", err)
	}
}

// TestSelectStartPort_DashboardOccupantRefuses is the mode distinction that
// must not regress: a real MusterFlow dashboard on the requested port is never
// bypassed, because a second dashboard on another port would serve a second
// store and silently split the CLI from the user's data.
func TestSelectStartPort_DashboardOccupantRefuses(t *testing.T) {
	srv := musterflowHealthServer(t)
	base := srv.Listener.Addr().(*net.TCPAddr).Port

	choice, err := selectStartPort(base, dashboardPortOccupied)
	if err == nil {
		t.Fatalf("expected refusal when a MusterFlow dashboard holds port %d, got port %d", base, choice.Port)
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("error %q does not say a dashboard is already running", err)
	}
	if !strings.Contains(err.Error(), "--dashboard-addr") {
		t.Errorf("error %q does not point at --dashboard-addr", err)
	}
	// No fallback happened: the refusal carries no chosen port and no fallback
	// note. (Do not probe base+1 for a free listener — httptest's ephemeral base
	// port can be handed to an unrelated listener, which made this test flake.)
	if choice.Port != 0 {
		t.Errorf("expected no chosen port when refusing, got %d", choice.Port)
	}
	if choice.Note != "" {
		t.Errorf("expected no fallback note when refusing, got %q", choice.Note)
	}
}
