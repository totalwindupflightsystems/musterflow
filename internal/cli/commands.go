// Package cli provides smaller leaf CLI command constructors extracted
// from the root command file to keep it under 800 lines.
package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/completion"
	"github.com/totalwindupflightsystems/musterflow/internal/wasm"
)

func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion script",
		Long: `Generate shell completion script for musterflow.

To install:
  musterflow completion bash > ~/.bash_completion.d/musterflow
  musterflow completion zsh > ~/.zsh/completions/_musterflow
  musterflow completion fish > ~/.config/fish/completions/musterflow.fish

Or let musterflow install automatically on first run.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := completion.Shell(args[0])
			switch shell {
			case completion.ShellBash:
				return cmd.Root().GenBashCompletionV2(cmd.OutOrStdout(), false)
			case completion.ShellZsh:
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case completion.ShellFish:
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", args[0])
			}
		},
	}
}

func newExportCommand(registry *app.Registry) *cobra.Command {
	var outputPath string
	cmd := &cobra.Command{
		Use:   "export [path]",
		Short: "Export API registry to JSONL",
		Long:  "Export all connected APIs to a JSONL file (one JSON object per line).\nDefault: ~/.musterflow/registry.jsonl",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := outputPath
			if path == "" {
				if len(args) > 0 {
					path = args[0]
				} else {
					path = filepath.Join(registry.DataDir(), "registry.jsonl")
				}
			}
			store := registry.Store()
			if store == nil {
				return fmt.Errorf("registry not loaded")
			}
			if err := app.ExportJSONL(store, path); err != nil {
				return err
			}
			conns := registry.List()
			fmt.Printf("✓ Exported %d APIs to %s\n", len(conns), path)
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path")
	return cmd
}

func newImportCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "import <path>",
		Short: "Import APIs from a JSONL file",
		Long:  "Import API connections from a JSONL file into the registry.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := registry.Store()
			if store == nil {
				return fmt.Errorf("registry not loaded")
			}
			n, err := app.ImportJSONL(store, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("✓ Imported %d APIs from %s\n", n, args[0])
			return nil
		},
	}
}

func newRefreshCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh <api-id>",
		Short: "Refresh API spec and regenerate commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dashboardBaseURL != "" {
				return refreshViaDashboard(args[0])
			}
			result, err := app.Refresh(cmd.Context(), registry, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("✓ Refreshed %s\n", result.Connection.Name)
			fmt.Printf("  Version: %s → %s", result.OldVersion, result.NewVersion)
			if result.VersionChanged {
				fmt.Print(" (changed)")
			}
			fmt.Println()
			fmt.Printf("  Endpoints: %d → %d\n", result.OldEndpoints, result.NewEndpoints)
			return nil
		},
	}
}

func newTransformCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transform",
		Short: "Manage WASM transforms",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List installed transforms",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := wasm.NewRegistry(filepath.Join(app.DefaultDataDir(), "transforms"))
			transforms, err := reg.List()
			if err != nil {
				return err
			}
			if len(transforms) == 0 {
				fmt.Println("No transforms installed.")
				fmt.Printf("Place .wasm files in %s/transforms/ to install.\n", app.DefaultDataDir())
				return nil
			}
			fmt.Println("Installed transforms:")
			for _, t := range transforms {
				fmt.Printf("  %s (%s)\n", t.Name, t.Path)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "install <catalog-entry>",
		Short: "Install a transform from the catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := wasm.NewRegistry(filepath.Join(app.DefaultDataDir(), "transforms"))
			if err := reg.InstallFromCatalog(args[0]); err != nil {
				fmt.Println(err)
			}
			return nil
		},
	})

	return cmd
}
