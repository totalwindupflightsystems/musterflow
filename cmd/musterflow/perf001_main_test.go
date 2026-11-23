package main

// PERF-001 regression tests: the config timeout key translated into the
// engine's per-attempt MUSTER_HTTP_TIMEOUT knob, and the one-shot command's
// signal-abort message.

import (
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

func TestApplyTimeoutEnv_UnsetKeyKeepsEngineDefaults(t *testing.T) {
	// Documented contract: when the key is unset the engine default (30s per
	// attempt, up to 3 attempts) applies and MUSTER_HTTP_TIMEOUT is untouched.
	t.Setenv(config.TimeoutEnvVar, "")
	applyTimeoutEnv(config.Config{Timeout: 0})
	if got := os.Getenv(config.TimeoutEnvVar); got != "" {
		t.Errorf("MUSTER_HTTP_TIMEOUT should stay unset when config timeout is unset, got %q", got)
	}
	applyTimeoutEnv(config.Config{Timeout: 5})
	if got := os.Getenv(config.TimeoutEnvVar); got != "5" {
		t.Errorf("MUSTER_HTTP_TIMEOUT should be translated to 5, got %q", got)
	}
	// An operator's own export wins over the config key.
	t.Setenv(config.TimeoutEnvVar, "12")
	applyTimeoutEnv(config.Config{Timeout: 5})
	if got := os.Getenv(config.TimeoutEnvVar); got != "12" {
		t.Errorf("operator MUSTER_HTTP_TIMEOUT=12 should keep precedence, got %q", got)
	}
}

func TestSignalAbortMessage_NamesTheInterruption(t *testing.T) {
	if msg := signalAbortMessage(syscall.SIGINT); msg == "" || !strings.Contains(msg, "SIGINT") || !strings.Contains(msg, "interrupted") {
		t.Errorf("signalAbortMessage(SIGINT) = %q, want a message naming the interruption", msg)
	}
	if msg := signalAbortMessage(syscall.SIGTERM); msg == "" || !strings.Contains(msg, "SIGTERM") {
		t.Errorf("signalAbortMessage(SIGTERM) = %q, want a message naming the signal", msg)
	}
}
