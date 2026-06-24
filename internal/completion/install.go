// Package completion provides shell completion support for MusterFlow.
// Supports bash, zsh, and fish with auto-install on first run.
package completion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Shell represents a supported shell type.
type Shell string

const (
	ShellBash Shell = "bash"
	ShellZsh  Shell = "zsh"
	ShellFish Shell = "fish"
)

// Generate produces a completion script for the given shell.
// cmd is the root cobra command to generate completions for.
// GenerateCompletion is a function type to avoid import cycles.
type GenerateCompletion func(shell Shell) (string, error)

// InstalledShells returns a list of shells that already have musterflow
// completions installed, detected by checking common installation paths.
func InstalledShells() []Shell {
	var installed []Shell
	home, err := os.UserHomeDir()
	if err != nil {
		return installed
	}

	checks := map[Shell][]string{
		ShellBash: {
			filepath.Join(home, ".bash_completion.d", "musterflow"),
			filepath.Join(home, ".bash_completion"),
		},
		ShellZsh: {
			filepath.Join(home, ".zsh", "completions", "_musterflow"),
			filepath.Join(home, ".zfunc", "_musterflow"),
		},
		ShellFish: {
			filepath.Join(home, ".config", "fish", "completions", "musterflow.fish"),
		},
	}

	for shell, paths := range checks {
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				installed = append(installed, shell)
				break
			}
		}
	}
	return installed
}

// InstallPath returns the recommended installation path for a shell.
func InstallPath(shell Shell) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}

	switch shell {
	case ShellBash:
		dir := filepath.Join(home, ".bash_completion.d")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("create bash completion dir: %w", err)
		}
		return filepath.Join(dir, "musterflow"), nil
	case ShellZsh:
		dir := filepath.Join(home, ".zsh", "completions")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("create zsh completion dir: %w", err)
		}
		return filepath.Join(dir, "_musterflow"), nil
	case ShellFish:
		dir := filepath.Join(home, ".config", "fish", "completions")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("create fish completion dir: %w", err)
		}
		return filepath.Join(dir, "musterflow.fish"), nil
	default:
		return "", fmt.Errorf("unsupported shell: %s", shell)
	}
}

// DetectShell returns the user's current shell from $SHELL.
func DetectShell() Shell {
	shell := os.Getenv("SHELL")
	switch {
	case strings.Contains(shell, "zsh"):
		return ShellZsh
	case strings.Contains(shell, "fish"):
		return ShellFish
	default:
		return ShellBash
	}
}

// Install writes the completion script for the shell to the appropriate location.
func Install(shell Shell, generate GenerateCompletion) error {
	script, err := generate(shell)
	if err != nil {
		return fmt.Errorf("generate %s completion: %w", shell, err)
	}
	if script == "" {
		return fmt.Errorf("empty completion script for %s", shell)
	}

	path, err := InstallPath(shell)
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		return fmt.Errorf("write %s completion: %w", shell, err)
	}

	fmt.Printf("✓ %s completions installed at %s\n", shell, path)
	fmt.Println("  Restart your shell or run: source", path)
	return nil
}

// ShouldPrompt returns true if completions should be prompted for install.
// Returns false if already installed or if auto_completion is disabled.
func ShouldPrompt(autoCompletionEnabled bool) bool {
	if !autoCompletionEnabled {
		return false
	}
	installed := InstalledShells()
	return len(installed) == 0
}

// PromptInstall asks the user if they want to install completions.
// Returns true if the user accepted.
func PromptInstall(shell Shell) bool {
	fmt.Printf("\n🔧 Shell completions not found. Install %s completions? [Y/n] ", shell)
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "" || response == "y" || response == "yes"
}
