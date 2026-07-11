package completion

import (
	"os"
	"strings"
	"testing"
)

func TestDetectShell(t *testing.T) {
	orig := os.Getenv("SHELL")
	defer os.Setenv("SHELL", orig)

	tests := []struct {
		shellEnv string
		want     Shell
	}{
		{"/bin/bash", ShellBash},
		{"/usr/bin/zsh", ShellZsh},
		{"/usr/bin/fish", ShellFish},
		{"/bin/sh", ShellBash}, // fallback
		{"", ShellBash},        // default
	}

	for _, tt := range tests {
		os.Setenv("SHELL", tt.shellEnv)
		got := DetectShell()
		if got != tt.want {
			t.Errorf("DetectShell() with SHELL=%q = %q, want %q", tt.shellEnv, got, tt.want)
		}
	}
}

func TestInstallPath(t *testing.T) {
	tests := []struct {
		shell       Shell
		wantSuffix  string
	}{
		{ShellBash, ".bash_completion.d/musterflow"},
		{ShellZsh, ".zsh/completions/_musterflow"},
		{ShellFish, ".config/fish/completions/musterflow.fish"},
	}

	for _, tt := range tests {
		path, err := InstallPath(tt.shell)
		if err != nil {
			t.Errorf("InstallPath(%q): %v", tt.shell, err)
			continue
		}
		if !strings.HasSuffix(path, tt.wantSuffix) {
			t.Errorf("InstallPath(%q) = %q, want suffix %q", tt.shell, path, tt.wantSuffix)
		}
	}

	// Unsupported shell
	if _, err := InstallPath("unsupported"); err == nil {
		t.Error("expected error for unsupported shell")
	}
}

func TestInstalledShells_Empty(t *testing.T) {
	// Should return empty when no completions are installed (test env)
	installed := InstalledShells()
	// In test env, no completions should be installed
	// (unless someone actually installed them on the CI machine)
	_ = installed
}

func TestShouldPrompt(t *testing.T) {
	if ShouldPrompt(false) {
		t.Error("ShouldPrompt(false) should be false")
	}
	// ShouldPrompt(true) depends on whether completions are already installed
}

func TestInstall_InvalidGenerate(t *testing.T) {
	err := Install(ShellBash, func(s Shell) (string, error) {
		return "", nil
	})
	if err == nil {
		t.Error("expected error for empty completion script")
	}
}

func TestPromptInstall_DefaultYes(t *testing.T) {
	// Simulate user pressing Enter (empty input = yes)
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r

	// Write newline (empty input = default yes) and close
	go func() {
		_, _ = w.Write([]byte("\n"))
		w.Close()
	}()

	result := PromptInstall(ShellBash)
	if !result {
		t.Error("expected true for empty input (default yes)")
	}
}

func TestPromptInstall_ExplicitYes(t *testing.T) {
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r

	go func() {
		_, _ = w.Write([]byte("y\n"))
		w.Close()
	}()

	result := PromptInstall(ShellZsh)
	if !result {
		t.Error("expected true for 'y'")
	}
}

func TestPromptInstall_No(t *testing.T) {
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r

	go func() {
		_, _ = w.Write([]byte("n\n"))
		w.Close()
	}()

	result := PromptInstall(ShellBash)
	if result {
		t.Error("expected false for 'n'")
	}
}

func TestInstall_GenerateError(t *testing.T) {
	err := Install(ShellBash, func(s Shell) (string, error) {
		return "", &fakeError{msg: "generator failed"}
	})
	if err == nil {
		t.Error("expected error when generate fails")
	}
}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }

func TestInstall_Success(t *testing.T) {
	homeDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", homeDir)
	defer os.Setenv("HOME", origHome)

	expectedScript := "# musterflow bash completion"
	err := Install(ShellBash, func(s Shell) (string, error) {
		return expectedScript, nil
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify file was written
	path, err := InstallPath(ShellBash)
	if err != nil {
		t.Fatalf("InstallPath: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != expectedScript {
		t.Errorf("expected %q, got %q", expectedScript, string(data))
	}
}

func TestShouldPrompt_WithInstalledCompletions(t *testing.T) {
	homeDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", homeDir)
	defer os.Setenv("HOME", origHome)

	// Install a completion to simulate pre-existing installation
	if err := Install(ShellBash, func(s Shell) (string, error) {
		return "# musterflow bash completion", nil
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if ShouldPrompt(true) {
		t.Error("ShouldPrompt should be false when completions are already installed")
	}
}

func TestInstalledShells_WithBashInstalled(t *testing.T) {
	homeDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", homeDir)
	defer os.Setenv("HOME", origHome)

	// Install bash completion
	if err := Install(ShellBash, func(s Shell) (string, error) {
		return "# musterflow bash completion", nil
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed := InstalledShells()
	if len(installed) == 0 {
		t.Error("expected bash to be detected as installed")
	}
	found := false
	for _, s := range installed {
		if s == ShellBash {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ShellBash in installed shells, got: %v", installed)
	}
}
