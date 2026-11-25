// Package retrypin pins the engine's retry ceiling (MUSTER_MAX_RETRIES) around
// generated write calls (DF-044).
//
// The engine (github.com/wojons/muster) resolves its retry behavior from the
// MUSTER_MAX_RETRIES environment variable inside pkg/request/builder.Execute
// via client.MaxAttemptsFromEnv, and its builder opts into
// client.WithNonIdempotentReplay whenever the ceiling is above one attempt.
// The default ceiling is 3 attempts, so a generated POST/PATCH against a
// backend that applies a write and then answers 5xx replays the same write up
// to three times (historical repro: .coding-hermes/evidence/2026-11-16-dogfood-r7).
//
// The pin holds the variable at 0 (a hard disable: one attempt, no retries)
// around the HTTP call only, and restores whatever the operator set
// afterwards. Idempotent methods (GET/HEAD/OPTIONS/TRACE/DELETE/PUT) are left
// at the engine default — retrying them cannot duplicate a write. Non-idempotent
// writes (POST/PATCH) send exactly one request unless a caller explicitly opts
// back into replay by passing --retries >= 2 (CLI surface).
package retrypin

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvVar is the engine variable that resolves the attempt ceiling at request
// time (pkg/client/client.go, MaxAttemptsFromEnv).
const EnvVar = "MUSTER_MAX_RETRIES"

// NotSet is the "no explicit value" sentinel for a caller's retry setting
// (the CLI flag's default). Methods keep the pin's default behavior: writes
// one attempt, idempotent calls the engine default ceiling.
const NotSet = -1

// methodReplayable reports whether a method is safe to replay. HTTP semantics:
// GET/HEAD/OPTIONS/TRACE are safe, PUT/DELETE are idempotent, POST and PATCH
// may have been applied upstream even when the attempt failed.
func methodReplayable(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PATCH":
		return false
	default:
		return true
	}
}

// Pin resolves the EnvVar value a request should run under.
//
// The second return is false when the environment must be left exactly as the
// operator set it:
//
//   - replayable method (GET/…), retries == NotSet → false (engine default)
//   - replayable method, retries >= 0             → true, value = retries
//     (0 hard-disables, N is the attempt ceiling)
//   - POST/PATCH, retries >= 2                    → false (explicit --retries
//     opt-in to write replay; hazard documented in docs/integration-guide.md)
//   - POST/PATCH, otherwise                       → true, value = "0" (the
//     default single-attempt write)
func Pin(method string, retries int) (value string, pin bool) {
	if methodReplayable(method) {
		if retries < 0 {
			return "", false
		}
		return strconv.Itoa(retries), true
	}
	if retries >= 2 {
		return "", false
	}
	return "0", true
}

// Hold pins EnvVar at value for the duration of call and restores whatever the
// operator set afterwards (remove the variable entirely if it was unset
// before, so pins never leak into later calls in the same process).
func Hold(value string, call func() error) error {
	old, had := os.LookupEnv(EnvVar)
	if err := os.Setenv(EnvVar, value); err != nil {
		return fmt.Errorf("pin %s=%s: %w", EnvVar, value, err)
	}
	defer func() {
		if had {
			_ = os.Setenv(EnvVar, old)
		} else {
			_ = os.Unsetenv(EnvVar)
		}
	}()
	return call()
}

// Around runs call under the pin the method and caller retry setting resolve
// to, and returns the call's outcome unchanged when no pin applies.
func Around(method string, retries int, call func() error) error {
	value, pin := Pin(method, retries)
	if !pin {
		return call()
	}
	return Hold(value, call)
}
