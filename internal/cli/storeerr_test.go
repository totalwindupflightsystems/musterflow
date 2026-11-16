package cli

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// lockErrorText is the DuckDB conflict text for pid, in the shape go-duckdb
// produces when a dashboard holds the store lock (DF-045).
func lockErrorText(pid int) string {
	return fmt.Sprintf(`open store: open duckdb: database/sql/driver: could not connect to database: `+
		`duckdb error: IO Error: Could not set lock on file "/tmp/mf-df045/data/musterflow.db": `+
		`Conflicting lock is held in /tmp/mf-df045/bin/musterflow (PID %d) by user kara. `+
		`See also https://duckdb.org/docs/connect/concurrency`, pid)
}

// withStoreError installs a store-open failure for the duration of a test. It
// is the same global main sets after registry.Load() fails, so a non-nil error
// from run() here is exactly what makes main exit 1.
func withStoreError(t *testing.T, text string) {
	t.Helper()
	SetStoreError(errors.New(text))
	t.Cleanup(func() { SetStoreError(nil) })
}

func newFailedStoreRoot(t *testing.T) *app.Registry {
	t.Helper()
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	SetDashboardURL("")
	t.Cleanup(func() { SetDashboardURL("") })
	return r
}

// AC 1 and AC 4: with the store locked, `list` must not claim an empty
// registry; it must return an error naming the holder PID and the fix.
func TestListFailsWhenStoreIsLocked(t *testing.T) {
	r := newFailedStoreRoot(t)
	withStoreError(t, lockErrorText(424242))

	root := NewRootCommand(r)
	root.SetArgs([]string{"list"})

	var execErr error
	out := captureStdout(func() { execErr = ResolveExecutionError(root.Execute()) })

	if execErr == nil {
		t.Fatalf("list returned nil error while the store was locked; output:\n%s", out)
	}
	if strings.Contains(out, "No APIs connected.") {
		t.Errorf("list still printed 'No APIs connected.' while the store was locked:\n%s", out)
	}
	msg := execErr.Error()
	for _, want := range []string{
		"cannot read the API registry",
		"locked by another musterflow process",
		"PID 424242",
		"musterflow config set port",
		"--dashboard-addr",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("lock error missing %q:\n%s", want, msg)
		}
	}
}

// AC 2: a generated command must not degrade to `unknown command "<api>"`.
func TestGeneratedCommandReportsStoreLock(t *testing.T) {
	r := newFailedStoreRoot(t)
	withStoreError(t, lockErrorText(424242))

	root := NewRootCommand(r)
	root.SetArgs([]string{"dogfood-items-api", "items"})

	var rawErr, err error
	out := captureStdout(func() {
		rawErr = root.Execute()
		err = ResolveExecutionError(rawErr)
	})

	if rawErr == nil {
		t.Fatalf("generated command on a locked store returned nil error; output:\n%s", out)
	}
	if !strings.Contains(rawErr.Error(), "unknown command") {
		t.Fatalf("test does not exercise the unknown-command path: %v", rawErr)
	}
	if err == nil {
		t.Fatal("resolved error is nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "unknown command") {
		t.Errorf("error still reports an unknown command instead of the lock:\n%s", msg)
	}
	for _, want := range []string{
		"locked by another musterflow process",
		"PID 424242",
		"musterflow config set port",
		"--dashboard-addr",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("lock error missing %q:\n%s", want, msg)
		}
	}
}

// AC 1/2: when the lock holder's port can be found, the message names that
// exact port and the --dashboard-addr that works, not a placeholder.
func TestStoreLockErrorNamesDiscoveredPort(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	withStoreError(t, lockErrorText(os.Getpid()))

	err := StoreUnavailable()
	if err == nil {
		t.Fatal("StoreUnavailable returned nil for a locked store")
	}
	msg := err.Error()
	for _, want := range []string{
		fmt.Sprintf("musterflow config set port %d", port),
		fmt.Sprintf("--dashboard-addr 127.0.0.1:%d", port),
		fmt.Sprintf("PID %d", os.Getpid()),
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("lock error missing %q:\n%s", want, msg)
		}
	}
}

// Unknown subcommands of real commands are not registry-dependent and must keep
// cobra's own behavior. Baseline on master: `config` has no RunE, so `config
// bogus` prints the config help and exits 0 — this fix must not change that,
// and must not leak the lock message into it.
func TestUnrelatedUnknownCommandIsUnchanged(t *testing.T) {
	r := newFailedStoreRoot(t)
	withStoreError(t, lockErrorText(424242))

	root := NewRootCommand(r)
	root.SetArgs([]string{"config", "bogus"})

	var err error
	out := captureStdout(func() { err = ResolveExecutionError(root.Execute()) })

	if err != nil && !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error for a non-registry subcommand was rewritten:\n%s", err.Error())
	}
	if err != nil && strings.Contains(err.Error(), "locked by another") {
		t.Errorf("lock message leaked into a non-registry subcommand error:\n%s", err.Error())
	}
	if strings.Contains(out, "locked by another") {
		t.Errorf("lock message leaked into non-registry output:\n%s", out)
	}
}

// AC 3: with a healthy store nothing changes — an empty registry still reports
// itself as empty and exits cleanly, and no lock warning appears.
func TestListWithHealthyStoreUnchanged(t *testing.T) {
	r := newFailedStoreRoot(t)
	SetStoreError(nil)

	root := NewRootCommand(r)
	root.SetArgs([]string{"list"})

	var err error
	out := captureStdout(func() { err = ResolveExecutionError(root.Execute()) })

	if err != nil {
		t.Fatalf("list with a healthy store returned %v; output:\n%s", err, out)
	}
	if !strings.Contains(out, "No APIs connected.") {
		t.Errorf("healthy empty registry did not report itself as empty; output:\n%s", out)
	}
	if strings.Contains(out, "locked by another") {
		t.Errorf("lock warning printed with a healthy store; output:\n%s", out)
	}
}

// A store failure that is not a lock conflict still fails closed, with the
// underlying cause attached.
func TestStoreUnavailableWrapsNonLockError(t *testing.T) {
	withStoreError(t, "open store: permission denied")

	err := StoreUnavailable()
	if err == nil {
		t.Fatal("StoreUnavailable returned nil")
	}
	if !strings.Contains(err.Error(), "cannot read the API registry: open store: permission denied") {
		t.Errorf("unexpected message: %s", err.Error())
	}
	if strings.Contains(err.Error(), "config set port") {
		t.Errorf("lock advice printed for a non-lock failure: %s", err.Error())
	}
}

func TestStoreUnavailableHealthy(t *testing.T) {
	SetStoreError(nil)
	if err := StoreUnavailable(); err != nil {
		t.Errorf("StoreUnavailable with a healthy store = %v, want nil", err)
	}
}

func TestResolveExecutionErrorPassesThroughWhenStoreHealthy(t *testing.T) {
	SetStoreError(nil)
	boom := errors.New(`unknown command "x" for "musterflow"`)
	if got := ResolveExecutionError(boom); got != boom {
		t.Errorf("ResolveExecutionError rewrote %v into %v with a healthy store", boom, got)
	}
}
