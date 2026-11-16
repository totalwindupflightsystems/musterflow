package app

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realLockError reproduces the text go-duckdb produced when a dashboard held
// the store lock in the DF-045 reproduction.
const realLockError = `open store: open duckdb: database/sql/driver: could not connect to database: ` +
	`duckdb error: IO Error: Could not set lock on file "/tmp/mf-1816/data/musterflow.db": ` +
	`Conflicting lock is held in /tmp/mf-1816/bin/musterflow (PID 3312052) by user kara. ` +
	`See also https://duckdb.org/docs/connect/concurrency`

func TestAsStoreLockParsesDuckDBConflict(t *testing.T) {
	lock, ok := AsStoreLock(errors.New(realLockError))
	if !ok {
		t.Fatalf("AsStoreLock did not classify a DuckDB lock conflict")
	}
	if lock.DBPath != "/tmp/mf-1816/data/musterflow.db" {
		t.Errorf("DBPath = %q, want /tmp/mf-1816/data/musterflow.db", lock.DBPath)
	}
	if lock.Holder != "/tmp/mf-1816/bin/musterflow" {
		t.Errorf("Holder = %q, want /tmp/mf-1816/bin/musterflow", lock.Holder)
	}
	if lock.PID != 3312052 {
		t.Errorf("PID = %d, want 3312052", lock.PID)
	}
	if lock.Err == nil {
		t.Error("Err = nil, want the original error")
	}
}

func TestAsStoreLockRejectsOtherStoreErrors(t *testing.T) {
	errs := []error{
		nil,
		errors.New("open store: permission denied"),
		errors.New("open store: duckdb error: IO Error: Failed to open database"),
		errors.New("unrelated"),
	}
	for _, err := range errs {
		if lock, ok := AsStoreLock(err); ok {
			t.Errorf("AsStoreLock(%v) = %+v, want not a lock conflict", err, lock)
		}
	}
}

func TestListenPortsSelectsListeningSockets(t *testing.T) {
	const table = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111111 1 0000000000000000 100 0 0 10 0
   1: 00000000:1F91 0A000001:AB12 01 00000000:00000000 00:00000000 00000000  1000        0 222222 1 0000000000000000 100 0 0 10 0
   2: 00000000:1F92 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 333333 1 0000000000000000 100 0 0 10 0
   3: 00000000000000000000000001000000:1F93 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 111111 1 0000000000000000 100 0 0 10 0
`
	path := filepath.Join(t.TempDir(), "tcp")
	if err := os.WriteFile(path, []byte(table), 0o600); err != nil {
		t.Fatalf("write table: %v", err)
	}

	ports := listenPorts(path, map[string]bool{"111111": true})
	want := []int{0x1F90, 0x1F93}
	if len(ports) != len(want) {
		t.Fatalf("listenPorts = %v, want %v", ports, want)
	}
	for i := range want {
		if ports[i] != want[i] {
			t.Fatalf("listenPorts = %v, want %v", ports, want)
		}
	}

	if got := listenPorts(filepath.Join(t.TempDir(), "missing"), map[string]bool{"111111": true}); got != nil {
		t.Errorf("listenPorts on missing table = %v, want nil", got)
	}
	if got := listenPorts(path, nil); got != nil {
		t.Errorf("listenPorts with no inodes = %v, want nil", got)
	}
}

func TestIsDashboardHealth(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"ok","connected_apis":2}`)
	}))
	defer healthy.Close()
	if !isDashboardHealth(portOf(t, healthy)) {
		t.Error("isDashboardHealth = false for a MusterFlow health endpoint, want true")
	}

	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer unhealthy.Close()
	if isDashboardHealth(portOf(t, unhealthy)) {
		t.Error("isDashboardHealth = true for a 500 endpoint, want false")
	}

	// A listener that answers 200 with unrelated JSON is not a dashboard.
	impostor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"hello":"world"}`)
	}))
	defer impostor.Close()
	if isDashboardHealth(portOf(t, impostor)) {
		t.Error("isDashboardHealth = true for a non-MusterFlow endpoint, want false")
	}
}

func TestListeningPortForPIDUnknown(t *testing.T) {
	for _, pid := range []int{0, -1, 4194303} {
		if got := listeningPortForPID(pid); got != 0 {
			t.Errorf("listeningPortForPID(%d) = %d, want 0", pid, got)
		}
	}
}

// TestListeningPortForPIDFindsDashboard checks the /proc walk end to end: the
// port reported for the test process is the one serving /api/health.
func TestListeningPortForPIDFindsDashboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer srv.Close()

	got := listeningPortForPID(os.Getpid())
	if got == 0 {
		t.Fatal("listeningPortForPID(test pid) = 0, want the health server's port")
	}
	if !isDashboardHealth(got) {
		t.Fatalf("listeningPortForPID returned %d, which is not a health endpoint", got)
	}
}

func portOf(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	addr, ok := srv.Listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", srv.Listener.Addr())
	}
	return addr.Port
}

// TestStoreLockMessageDetails keeps the parsed fields usable without a live
// process: no port means the caller falls back to the generic fix text.
func TestStoreLockDetailsWithoutLiveProcess(t *testing.T) {
	lock, ok := AsStoreLock(errors.New(strings.Replace(realLockError, "3312052", "4194303", 1)))
	if !ok || lock.PID != 4194303 {
		t.Fatalf("AsStoreLock = %+v, ok=%v", lock, ok)
	}
	if lock.Port != 0 {
		t.Errorf("Port = %d for a dead PID, want 0", lock.Port)
	}
}
