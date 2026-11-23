package cli

// Operation watchdog (PERF-001).
//
// The engine's generated command handlers capture the request context
// themselves (pkg/generator createRunHandler uses context.Background() and
// ignores cmd.Context()), so musterflow can neither inject a deadline nor a
// signal-notify context into the real `musterflow <api> <op>` path. The two
// musterflow-owned seams that DO reach that path are:
//
//  1. MUSTER_HTTP_TIMEOUT — the engine reads it from the environment per
//     request, so main.go translates the config timeout into a per-attempt
//     cap (an operator's own export keeps precedence).
//  2. this watchdog — armed by executeLeaf just before a generated leaf
//     executes and disarmed when it returns. It bounds the WHOLE call
//     (attempts + retry backoff), which the per-attempt env knob alone does
//     not (3 × 5 s attempts + backoff would still overshoot a 5 s timeout).
//
// Both are armed only when the `timeout` config key is set: unset keeps
// byte-for-byte today's engine behavior.
import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"
)

// operationTimeoutSeconds is the config timeout translated by main.go via
// SetOperationTimeout. 0 (default) disables the watchdog entirely.
var operationTimeoutSeconds int

// SetOperationTimeout arms generated-operation watchdogs at <secs> seconds
// (whole-call ceiling). Values <= 0 restore the unwatched behavior.
func SetOperationTimeout(secs int) {
	if secs < 0 {
		secs = 0
	}
	operationTimeoutSeconds = secs
}

// watchdogExit is the process-exit seam so tests can observe a fire without
// killing the test process.
var watchdogExit = os.Exit

// operationWatchdogClock schedules the fire — default time.AfterFunc; tests
// stub it with short timers plus a recorded exit sink.
var operationWatchdogClock = time.AfterFunc

// armOperationWatchdog starts a whole-call watchdog if the command is a
// generated API operation and the config timeout is set; otherwise it returns
// nil and nothing changes.
func armOperationWatchdog(target *cobra.Command) func() {
	if operationTimeoutSeconds <= 0 {
		return nil
	}
	if target == nil || target.Annotations[generator.AnnotationOperationPath] == "" {
		// Not a generated operation (group help, unknown leaf, ...) — no
		// watchdog; those commands do not make a generated API call.
		return nil
	}
	secs := operationTimeoutSeconds
	timer := operationWatchdogClock(time.Duration(secs)*time.Second, func() {
		fmt.Fprintf(os.Stderr,
			"canceled: no upstream answer within the configured %d-second timeout — '%s' aborted. Raise it with 'musterflow config set timeout <seconds>' or check the API's base URL\n",
			secs, target.CommandPath())
		watchdogExit(1)
	})
	return func() { timer.Stop() }
}
