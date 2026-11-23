package cli

// PERF-001 regression tests: the `timeout` config key (whole-call ceiling on
// generated CLI API calls), signal-interruptable ExecuteAndFormat, and the
// generated-operation watchdog. No real signals are sent — the context seam
// and recorded sinks stand in.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/request"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

func TestExecuteAndFormat_SignalContextReturnsInterruptedError(t *testing.T) {
	// The upstream stalls past any sane test budget; the cancelled execution
	// context must abort the call (exercised via the seam, not a real signal).
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(30 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer stalled.Close()

	orig := newExecContext
	newExecContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-cancelled: the stand-in for a received SIGINT
		return ctx, cancel
	}
	defer func() { newExecContext = orig }()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	builder := request.NewBuilder(stalled.URL, "/items", "GET")

	err := ExecuteAndFormat(cmd, builder, ExecuteOptions{Format: "json"})
	if err == nil {
		t.Fatal("expected an error when the execution context is cancelled, got nil")
	}
	if !strings.Contains(err.Error(), "interrupted by SIGINT") {
		t.Errorf("expected the interruption to be named in the error, got: %q", err.Error())
	}
}

func TestExecuteAndFormat_TimeoutDeadlineReturnsActionableError(t *testing.T) {
	// Upstream answers after 2s; the configured whole-call deadline of 1s
	// must abort with an error that names the timeout.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-time.After(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"k":"v"}`))
	}))
	defer slow.Close()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	builder := request.NewBuilder(slow.URL, "/items", "GET")

	start := time.Now()
	err := ExecuteAndFormat(cmd, builder, ExecuteOptions{Format: "json", TimeoutSeconds: 1})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected the timeout named in the error, got: %q", err.Error())
	}
	if elapsed >= 2*time.Second {
		t.Errorf("call took %s — the deadline did not bound it", elapsed)
	}
}

func TestExecuteAndFormat_BuilderTimeoutAppliedOnlyWhenKeySet(t *testing.T) {
	// The engine's Builder.WithTimeout is currently a no-op, so the observable
	// contract is musterflow's decision: applied when TimeoutSeconds > 0 and
	// NOT applied when unset. Recorded via the seam.
	observed := 0
	notObserved := 0
	orig := applyBuilderTimeout
	applyBuilderTimeout = func(b *request.Builder, secs int) {
		observed++
	}
	defer func() { applyBuilderTimeout = orig }()

	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"k":"v"}`))
	}))
	defer fast.Close()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	// Set: WithTimeout forwarded with the configured seconds.
	if err := ExecuteAndFormat(cmd, request.NewBuilder(fast.URL, "/items", "GET"), ExecuteOptions{Format: "json", TimeoutSeconds: 5}); err != nil {
		t.Fatalf("ExecuteAndFormat with timeout=5: %v", err)
	}
	if observed != 1 {
		t.Errorf("expected builder.WithTimeout applied exactly once when the key is set, got %d", observed)
	}

	// Unset: untouched.
	if err := ExecuteAndFormat(cmd, request.NewBuilder(fast.URL, "/items", "GET"), ExecuteOptions{Format: "json"}); err != nil {
		t.Fatalf("ExecuteAndFormat without timeout: %v", err)
	}
	if notObserved != 0 || observed != 1 {
		t.Errorf("expected no builder.WithTimeout call when the key is unset (calls=%d)", observed-1)
	}
}

func TestConfigSetTimeout_PersistsValidatesAndShows(t *testing.T) {
	home := t.TempDir()
	defer setHome(t, home)()

	r := app.NewRegistry(home)
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Valid value persists.
	root := NewRootCommand(r)
	root.SetArgs([]string{"config", "set", "timeout", "5"})
	if out := captureStdout(func() {
		if err := root.Execute(); err != nil {
			t.Fatalf("config set timeout 5: %v", err)
		}
	}); !strings.Contains(out, "Set timeout = 5") {
		t.Errorf("expected set confirmation, got: %q", out)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if loaded.Timeout != 5 {
		t.Errorf("config file did not persist timeout=5, got %d", loaded.Timeout)
	}

	// show displays it.
	root = NewRootCommand(r)
	root.SetArgs([]string{"config", "show"})
	out := captureStdout(func() {
		if err := root.Execute(); err != nil {
			t.Fatalf("config show: %v", err)
		}
	})
	if !strings.Contains(out, "Timeout:           5") {
		t.Errorf("config show does not display timeout=5, got:\n%s", out)
	}

	// Invalid values fail with a clear error. (For -3 the failure surfaces
	// from pflag before the validator sees the value — "unknown shorthand
	// flag: '3'" — which still fails non-zero naming the input; the
	// validator's own rejection of -3 is covered by
	// TestParseTimeoutSeconds_ConfigPackage.)
	for _, bad := range []string{"0", "abc"} {
		root = NewRootCommand(r)
		root.SetArgs([]string{"config", "set", "timeout", bad})
		err := root.Execute()
		if err == nil {
			t.Errorf("config set timeout %q: expected error, got nil", bad)
			continue
		}
		if !strings.Contains(err.Error(), "invalid timeout") {
			t.Errorf("config set timeout %q: expected 'invalid timeout' error, got: %q", bad, err.Error())
		}
	}
	{
		root = NewRootCommand(r)
		root.SetArgs([]string{"config", "set", "timeout", "-3"})
		if err := root.Execute(); err == nil {
			t.Error("config set timeout -3: expected failure, got nil")
		}
	}

	// The unknown-key error lists timeout as valid.
	root = NewRootCommand(r)
	root.SetArgs([]string{"config", "set", "bogus", "value"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("unknown-key error should list timeout among valid keys, got: %v", err)
	}
}

func generatedOpCommand(t *testing.T, name string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: name}
	cmd.Annotations = map[string]string{
		generator.AnnotationOperationPath: "/items",
	}
	return cmd
}

func TestArmOperationWatchdog_GatedOnGeneratedOpAndTimeout(t *testing.T) {
	origClock := operationWatchdogClock
	operationWatchdogClock = func(d time.Duration, fn func()) *time.Timer {
		return time.AfterFunc(30*time.Millisecond, fn)
	}
	defer func() { operationWatchdogClock = origClock }()
	exits := make(chan int, 1)
	origExit := watchdogExit
	watchdogExit = func(code int) { exits <- code }
	defer func() { watchdogExit = origExit }()

	op := generatedOpCommand(t, "items")
	plain := &cobra.Command{Use: "not-an-op"}

	// Unset timeout: no watchdog anywhere.
	SetOperationTimeout(0)
	if stop := armOperationWatchdog(op); stop != nil {
		t.Error("expected no watchdog when the timeout key is unset")
	}

	// Set timeout but not a generated op: still no watchdog.
	SetOperationTimeout(1)
	if stop := armOperationWatchdog(plain); stop != nil {
		t.Error("expected no watchdog for a non-generated command")
	}

	// Generated op + timeout: watchdog arms, fires, and aborts with exit 1.
	stop := armOperationWatchdog(op)
	if stop == nil {
		t.Fatal("expected a watchdog when a generated op runs with the timeout set")
	}
	select {
	case code := <-exits:
		if code != 1 {
			t.Errorf("watchdog exit code = %d, want 1", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not fire within 2s")
	}
	// Disarm prevents a late fire (the sink already consumed the first; a
	// second fire would exit a real process — here nothing is expected).
	stop() // no-op for an already-fired timer, exercised for return-shape

	// A stopped watchdog never fires.
	SetOperationTimeout(1)
	stop2 := armOperationWatchdog(generatedOpCommand(t, "items2"))
	if stop2 == nil {
		t.Fatal("expected a watchdog for stop-test")
	}
	stop2()
	select {
	case code := <-exits:
		t.Errorf("stopped watchdog still fired with exit %d", code)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestParseTimeoutSeconds_ConfigPackage(t *testing.T) {
	if got, err := config.ParseTimeoutSeconds("5"); err != nil || got != 5 {
		t.Errorf("ParseTimeoutSeconds(5) = %d, %v; want 5, nil", got, err)
	}
	if got, err := config.ParseTimeoutSeconds(" 30 "); err != nil || got != 30 {
		t.Errorf("ParseTimeoutSeconds(' 30 ') = %d, %v; want 30, nil", got, err)
	}
	for _, bad := range []string{"0", "-3", "abc", ""} {
		_, err := config.ParseTimeoutSeconds(bad)
		if err == nil {
			t.Errorf("ParseTimeoutSeconds(%q) = nil, want error", bad)
		} else if !strings.Contains(err.Error(), "invalid timeout") {
			t.Errorf("ParseTimeoutSeconds(%q) error text = %q, want 'invalid timeout'", bad, err.Error())
		}
	}
}
