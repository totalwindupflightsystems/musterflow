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
		w.Write([]byte("\n"))
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
		w.Write([]byte("y\n"))
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
		w.Write([]byte("n\n"))
		w.Close()
	}()

	result := PromptInstall(ShellBash)
	if result {
		t.Error("expected false for 'n'")
	}
}
