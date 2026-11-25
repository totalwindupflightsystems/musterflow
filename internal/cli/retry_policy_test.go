package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/request"

	"github.com/totalwindupflightsystems/musterflow/internal/retrypin"
)

// postingServer builds an server whose POST handler counts every request and
// answers status `status` with an empty JSON body.
func postingServer(t *testing.T, status int) (*httptest.Server, *int64) {
	t.Helper()
	var hits int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"apply-then-fail"}`))
	}))
	t.Cleanup(ts.Close)
	return ts, &hits
}

// runExecute posts to the server through ExecuteAndFormat with the retry
// preference the test is exercising.
func runExecute(url string, retries *int) error {
	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	builder := request.NewBuilder(url, "/items", "POST")
	return ExecuteAndFormat(cmd, builder, ExecuteOptions{Format: "json", Retries: retries})
}

// TestExecuteAndFormat_WriteDoesNotReplayByDefault is T1: a POST against a
// backend that applies the write and then answers 500 must reach the server
// exactly once with default options (DF-044 — the historical dogfood-r7
// duplicate-row shape).
func TestExecuteAndFormat_WriteDoesNotReplayByDefault(t *testing.T) {
	ts, hits := postingServer(t, http.StatusInternalServerError)

	err := runExecute(ts.URL, nil)
	if err == nil {
		t.Fatal("expected the 500 to surface as an error")
	}
	if got := atomic.LoadInt64(hits); got != 1 {
		t.Errorf("POST reached the server %d times with default options; want 1", got)
	}
}

// TestExecuteAndFormat_WriteReplayOptIn is T2: --retries 3 is the explicit
// opt-in that lets the write replay on transient failures.
func TestExecuteAndFormat_WriteReplayOptIn(t *testing.T) {
	ts, hits := postingServer(t, http.StatusInternalServerError)

	retries := 3
	err := runExecute(ts.URL, &retries)
	if err == nil {
		t.Fatal("expected the exhausted retry ceiling to surface as an error")
	}
	if got := atomic.LoadInt64(hits); got != 3 {
		t.Errorf("POST reached the server %d times with --retries 3; want 3", got)
	}
}

// TestExecuteAndFormat_WriteSingleAttemptOnExplicitOne pins --retries 1 on a
// write: one attempt is not an opt-in to replay.
func TestExecuteAndFormat_WriteSingleAttemptOnExplicitOne(t *testing.T) {
	ts, hits := postingServer(t, http.StatusInternalServerError)

	retries := 1
	_ = runExecute(ts.URL, &retries)
	if got := atomic.LoadInt64(hits); got != 1 {
		t.Errorf("POST reached the server %d times with --retries 1; want 1", got)
	}
}

// TestExecuteAndFormat_ReadRetriesByDefault is T3: idempotent methods keep
// the engine's default attempt ceiling — a 502-then-200 GET succeeds.
func TestExecuteAndFormat_ReadRetriesByDefault(t *testing.T) {
	var hits int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&hits, 1)
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":1}]}`))
	}))
	defer ts.Close()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	builder := request.NewBuilder(ts.URL, "/items", "GET")

	if err := ExecuteAndFormat(cmd, builder, ExecuteOptions{Format: "json"}); err != nil {
		t.Fatalf("default read should have retried past the 502: %v", err)
	}
	if !strings.Contains(buf.String(), `"items"`) {
		t.Errorf("expected the retried GET's body in the output, got: %s", buf.String())
	}
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Errorf("GET reached the server %d times; want 2 (one 502 + one retry)", got)
	}
}

// TestExecuteAndFormat_ReadHardDisabled is T4: --retries 0 disables retrying
// for reads too and the error surfaces after exactly one attempt.
func TestExecuteAndFormat_ReadHardDisabled(t *testing.T) {
	var hits int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	cmd := &cobra.Command{}
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	builder := request.NewBuilder(ts.URL, "/items", "GET")

	retries := 0
	err := ExecuteAndFormat(cmd, builder, ExecuteOptions{Format: "json", Retries: &retries})
	if err == nil {
		t.Fatal("expected the 502 to surface as an error without retries")
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("GET reached the server %d times with --retries 0; want 1", got)
	}
}

// TestExecuteLeafPinHonorsMethodAndFlag exercises the generated-leaf seam:
// the pin is derived from the operation's method annotation plus the
// inherited --retries flag, and the environment is restored afterwards.
func TestExecuteLeafPinHonorsMethodAndFlag(t *testing.T) {
	t.Run("POST leaf runs pinned to one attempt", func(t *testing.T) {
		target := &cobra.Command{}
		target.Annotations = map[string]string{generator.AnnotationOperationMethod: "POST"}

		var observed string
		err := executeLeafUnderRetryPin(target, func() error {
			observed = os.Getenv(retrypin.EnvVar)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if observed != "0" {
			t.Errorf("POST leaf ran with %s=%q; want \"0\"", retrypin.EnvVar, observed)
		}
		if got := os.Getenv(retrypin.EnvVar); got != "" {
			t.Errorf("pin leaked: %s=%q after the leaf", retrypin.EnvVar, got)
		}
	})

	t.Run("POST leaf with --retries 3 runs unpinned", func(t *testing.T) {
		target := &cobra.Command{}
		target.Annotations = map[string]string{generator.AnnotationOperationMethod: "POST"}
		target.Flags().Int(retriesFlagName, retrypin.NotSet, "test")
		if err := target.ParseFlags([]string{"--retries", "3"}); err != nil {
			t.Fatal(err)
		}

		var observed string
		err := executeLeafUnderRetryPin(target, func() error {
			observed = os.Getenv(retrypin.EnvVar)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if observed != "" {
			t.Errorf("opt-in leaf must not touch %s; got %q", retrypin.EnvVar, observed)
		}
	})
}

func TestRetriesFlagValue(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Int(retriesFlagName, retrypin.NotSet, "test")
	if got := retriesFlagValue(cmd); got != retrypin.NotSet {
		t.Errorf("untouched flag: retriesFlagValue = %d; want %d", got, retrypin.NotSet)
	}
	if err := cmd.ParseFlags([]string{"--retries", "0"}); err != nil {
		t.Fatal(err)
	}
	if got := retriesFlagValue(cmd); got != 0 {
		t.Errorf("--retries 0: retriesFlagValue = %d; want 0", got)
	}

	cmd2 := &cobra.Command{}
	cmd2.Flags().Int(retriesFlagName, retrypin.NotSet, "test")
	if err := cmd2.ParseFlags([]string{"--retries", "7"}); err != nil {
		t.Fatal(err)
	}
	if got := retriesFlagValue(cmd2); got != 7 {
		t.Errorf("--retries 7: retriesFlagValue = %d; want 7", got)
	}
}
