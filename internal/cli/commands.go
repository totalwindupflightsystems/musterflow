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
			// If the dashboard is running, route through its HTTP API to
			// avoid DuckDB lock conflicts (the dashboard holds the write lock).
			if dashboardBaseURL != "" {
				return exportViaDashboard(path)
			}
			// DF-045: an unreadable store is not an empty registry — fail with
			// the lock cause instead of exporting zero APIs as if it were.
			if err := StoreUnavailable(); err != nil {
				return err
			}
			store := registry.Store()
			if store == nil {
				return fmt.Errorf("registry not loaded (the dashboard may be running — export/import route through the dashboard; if the dashboard is NOT running this is a bug)")
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
	var inputPath string
	cmd := &cobra.Command{
		Use:   "import [path]",
		Short: "Import APIs from a JSONL file",
		Long: "Import API connections from a JSONL file into the registry.\n" +
			"The file path may be given positionally (`import <path>`) or with --input (alias: --file).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// DF-034: accept a path flag as well as the positional form,
			// mirroring `export` (flag wins, positional fallback) so the
			// import/export pair exposes one flag shape.
			path := inputPath
			if path == "" {
				if len(args) > 0 {
					path = args[0]
				} else {
					return fmt.Errorf("import file required (positional <path>, --input, or --file)")
				}
			}
			// If the dashboard is running, route through its HTTP API to
			// avoid DuckDB lock conflicts (the dashboard holds the write lock).
			if dashboardBaseURL != "" {
				return importViaDashboard(path)
			}
			// DF-045: an unreadable store is not an empty registry; name the lock.
			if err := StoreUnavailable(); err != nil {
				return err
			}
			store := registry.Store()
			if store == nil {
				return fmt.Errorf("registry not loaded (the dashboard may be running — export/import route through the dashboard; if the dashboard is NOT running this is a bug)")
			}
			n, err := app.ImportJSONL(store, path)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Imported %d APIs from %s\n", n, path)
			return nil
		},
	}
	cmd.Flags().StringVar(&inputPath, "input", "", "Input JSONL file path (alternative to positional argument)")
	cmd.Flags().StringVar(&inputPath, "file", "", "Alias for --input")
	return cmd
}

func newRefreshCommand(registry *app.Registry) *cobra.Command {
	var tolerate bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "refresh <api-id|name>",
		Short: "Refresh API spec and regenerate commands",
		Long: "Re-fetch the OpenAPI spec for a connected API and update its version and endpoint count.\n" +
			"The API may be given by id (as shown by 'musterflow list') or by its name.\n\n" +
			"A connection made with --tolerate keeps that opt-in: refresh runs the same\n" +
			"cosmetic-defect repair pass and reports the same tolerated-defect summary.\n" +
			"Use --tolerate / --strict to change the opt-in for this run and every run after.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if tolerate && strict {
				return fmt.Errorf("--tolerate and --strict are mutually exclusive")
			}
			if dashboardBaseURL != "" {
				if tolerate || strict {
					return fmt.Errorf("--tolerate/--strict require a direct refresh: stop the dashboard (it is routing refreshes through %s) and retry", dashboardBaseURL)
				}
				return refreshViaDashboard(args[0])
			}
			// DF-045: an unreadable store is not "get api: not found".
			if err := StoreUnavailable(); err != nil {
				return err
			}
			// DF-024: resolve name-or-id so the name shown by 'musterflow list'
			// works here too. Return the resolution error unchanged: it names
			// the connected APIs, which is the actionable part.
			conn, err := registry.GetByNameOrID(args[0])
			if err != nil {
				return err
			}

			opts := app.RefreshOptions{}
			switch {
			case tolerate:
				on := true
				opts.Tolerate = &on
			case strict:
				off := false
				opts.Tolerate = &off
			}

			result, err := app.RefreshWithOptions(cmd.Context(), registry, conn.ID, opts)
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
			printToleratedDefects(result.Tolerated, "refreshed")
			return nil
		},
	}

	cmd.Flags().BoolVar(&tolerate, "tolerate", false,
		"Repair cosmetic spec defects before parsing and keep that opt-in for later refreshes")
	cmd.Flags().BoolVar(&strict, "strict", false,
		"Parse strictly from now on, clearing a previous --tolerate opt-in")
	return cmd
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
			// InstallFromCatalog reports the unimplemented-catalog placeholder as an
			// error; surface it so the CLI exits non-zero instead of printing and
			// reporting success (staticcheck SA4023: the error is never nil).
			return reg.InstallFromCatalog(args[0])
		},
	})

	return cmd
}
