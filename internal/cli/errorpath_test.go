package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// TestErrorPath_UnknownCommand verifies that an unknown root-level
// subcommand returns a non-nil error from Execute (so the process
// exits non-zero) and that the error message is NOT printed twice
// in the captured output. See DF-007.
func TestErrorPath_UnknownCommand(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	root := NewRootCommand(r)
	root.SetArgs([]string{"nonsense"})

	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)

	execErr := root.Execute()
	if execErr == nil {
		t.Fatal("expected non-nil error for unknown command, got nil")
	}

	combined := out.String() + errOut.String()

	// The error must appear at most once in combined output — not doubled.
	// cobra's SilenceErrors prevents it from printing; main.go prints once.
	// In tests, main.go is not in the loop, so cobra would normally print
	// once — but with SilenceErrors=true it prints zero times.
	// The error is returned from Execute, not written to stderr.
	// So combined output should NOT contain the error text at all
	// (SilenceErrors=true), or at most once.
	errorText := execErr.Error()
	count := strings.Count(combined, errorText)
	if count > 1 {
		t.Errorf("error text %q appears %d times in output (should be at most 1):\n%s",
			errorText, count, combined)
	}
}

// TestErrorPath_UnknownFlag verifies that an unknown flag returns a
// non-nil error from Execute and the error is not doubled in output.
// See DF-007.
func TestErrorPath_UnknownFlag(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	root := NewRootCommand(r)
	root.SetArgs([]string{"list", "--bogus-flag"})

	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)

	execErr := root.Execute()
	if execErr == nil {
		t.Fatal("expected non-nil error for unknown flag, got nil")
	}

	combined := out.String() + errOut.String()

	// With SilenceUsage + SilenceErrors, cobra should not print the
	// usage block or the error. The error is returned from Execute.
	// At most one occurrence of the error text.
	errorText := execErr.Error()
	count := strings.Count(combined, errorText)
	if count > 1 {
		t.Errorf("error text %q appears %d times in output (should be at most 1):\n%s",
			errorText, count, combined)
	}

	// Usage block should NOT be printed on flag errors with SilenceUsage=true.
	// The "Usage:" header is the marker for a usage dump.
	if strings.Count(combined, "Usage:") > 0 {
		t.Errorf("usage block should not be printed with SilenceUsage=true, but found in output:\n%s",
			combined)
	}
}

// TestErrorPath_UnknownSubcommandUnderAPI verifies that an unknown
// subcommand under a connected API parent command returns a non-nil
// error (exit non-zero) instead of printing help and exiting 0.
// See DF-007.
func TestErrorPath_UnknownSubcommandUnderAPI(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Add a fake connection so an API parent command is registered.
	// We don't need real spec data — the API parent command has
	// DisableFlagParsing and lazy-loads on first access.
	_ = r.Add(&app.APIConnection{
		ID:            "test-api-001",
		Name:          "test-api",
		SpecURL:       "http://127.0.0.1:1/nonexistent.json",
		BaseURL:       "http://127.0.0.1:1",
		AuthType:      "none",
		EndpointCount: 0,
	})

	root := NewRootCommand(r)
	root.SetArgs([]string{"test-api", "nonsense-subcommand"})

	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)

	execErr := root.Execute()
	if execErr == nil {
		t.Fatal("expected non-nil error for unknown subcommand under API parent, got nil")
	}
}

// TestErrorPath_SilenceFlagsSet verifies that the root command has
// SilenceUsage and SilenceErrors set to true after construction.
// See DF-007.
func TestErrorPath_SilenceFlagsSet(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	root := NewRootCommand(r)

	if !root.SilenceUsage {
		t.Error("expected root.SilenceUsage == true")
	}
	if !root.SilenceErrors {
		t.Error("expected root.SilenceErrors == true")
	}
}

// TestErrorPath_HelpStillWorks verifies that --help still works
// (exit 0, help text printed) after the SilenceUsage/SilenceErrors
// change. See DF-007.
func TestErrorPath_HelpStillWorks(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	root := NewRootCommand(r)
	root.SetArgs([]string{"--help"})

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	execErr := root.Execute()
	if execErr != nil {
		t.Fatalf("expected nil error for --help, got: %v", execErr)
	}

	output := out.String()
	if !strings.Contains(output, "MusterFlow turns any API") {
		t.Errorf("expected help text in output, got: %s", output)
	}
}

// TestErrorPath_ListHelpStillWorks verifies that subcommand --help
// still works after the SilenceUsage/SilenceErrors change.
// See DF-007.
func TestErrorPath_ListHelpStillWorks(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	root := NewRootCommand(r)
	root.SetArgs([]string{"list", "--help"})

	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	execErr := root.Execute()
	if execErr != nil {
		t.Fatalf("expected nil error for 'list --help', got: %v", execErr)
	}

	output := out.String()
	if !strings.Contains(output, "List connected APIs") {
		t.Errorf("expected 'List connected APIs' in help output, got: %s", output)
	}
}
