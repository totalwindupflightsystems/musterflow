// Package cli provides the cobra command tree for the musterflow CLI.
//
// Core command constructors. Large helper functions have been extracted:
//   - register.go: dashboard HTTP API routing helpers
//   - api.go: API subcommand loading and spec-parsing infrastructure
//   - execute.go: request building, execution, and output formatting
//   - formats.go: output format writers (CSV, JSONL, JSON, etc.)
//   - oauth.go: OAuth2 callback server
//   - commands.go: leaf command constructors (completion, export, import, refresh, transform)
package cli

import (
	"encoding/json"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/auth"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
	"github.com/totalwindupflightsystems/musterflow/internal/config"
	"github.com/totalwindupflightsystems/musterflow/internal/workflow"
)

// authMgr is set by the main function and used by BuildRequest for auto-auth.
var authMgr *auth.Manager

// SetAuthManager sets the global auth manager for auto-injecting credentials.
func SetAuthManager(m *auth.Manager) {
	authMgr = m
}

// flagDataDir holds the --data-dir persistent flag value, set by main via
// SetDataDir before ExecuteContext runs. When non-empty, config load/save
// operations in CLI handlers honor this directory instead of ~/.musterflow.
var flagDataDir string

// SetDataDir sets the global data directory override for CLI handlers.
// Called by main after parsing the --data-dir flag.
func SetDataDir(dir string) {
	flagDataDir = dir
}

// loadConfig loads the config, honoring the --data-dir flag if set.
func loadConfig() (config.Config, error) {
	if flagDataDir != "" {
		return config.LoadWithDataDir(flagDataDir)
	}
	return config.Load()
}

// configPath returns the config file path, honoring the --data-dir flag if set.
func configPath() string {
	if flagDataDir != "" {
		return config.ConfigPathFor(flagDataDir)
	}
	return config.ConfigPath()
}

// dashboardBaseURL is set by main when the dashboard is detected as running.
// When non-empty, CLI write operations (connect/disconnect) route through the
// dashboard HTTP API instead of opening the DB directly.
var dashboardBaseURL string

// SetDashboardURL sets the dashboard base URL so CLI write operations route
// through the dashboard API (avoiding DuckDB lock conflicts).
func SetDashboardURL(url string) {
	dashboardBaseURL = url
	installDashboardAuthTransport()
}

// auth command flags
var (
	typeFlag    string
	keyFlag     string
	certFlag    string
	keyPathFlag string
	// OAuth2 login flags
	oauthClientID     string
	oauthClientSecret string
	oauthAuthURL      string
	oauthTokenURL     string
	oauthScopes       []string
	oauthRedirectPort int
)

// NewRootCommand creates the root musterflow command.
func NewRootCommand(registry *app.Registry) *cobra.Command {
	root := &cobra.Command{
		Use:   "musterflow",
		Short: "Turn any API into a CLI, an MCP tool, and a workflow",
		Long: `MusterFlow turns any API into a typed CLI, an MCP endpoint, and a workflow engine.

Connect:    musterflow connect https://api.github.com
List:       musterflow list
Use:        musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available
Workflow:   musterflow flow create`,

		// SilenceUsage and SilenceErrors prevent cobra from printing
		// the error and usage itself. The error is instead returned from
		// ExecuteContext and printed exactly once by main.go. Without
		// these, cobra prints "Error: ..." + usage, then main() prints
		// "Error: ..." again — doubling the error output. See DF-007.
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newStartCommand(registry))
	root.AddCommand(newConnectCommand(registry))
	root.AddCommand(newListCommand(registry))
	root.AddCommand(newDisconnectCommand(registry))
	root.AddCommand(newCatalogCommand(registry))
	root.AddCommand(newFlowCommand(registry))
	root.AddCommand(newMCPCommand(registry))
	root.AddCommand(newConfigCommand(registry))
	root.AddCommand(newAuthCommand(registry))
	root.AddCommand(newCompletionCommand())
	root.AddCommand(newExportCommand(registry))
	root.AddCommand(newImportCommand(registry))
	root.AddCommand(newRefreshCommand(registry))
	root.AddCommand(newTransformCommand())

	root.PersistentFlags().StringVar(&outputFlag, "output-file", "", "Output file path (format auto-detected from extension)")

	loadAPISubcommands(root, registry)

	return root
}

var outputFlag string

func newStartCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the MusterFlow dashboard and MCP endpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Starting MusterFlow...")
			fmt.Println("Dashboard: http://localhost:9876")
			fmt.Println("MCP endpoint: http://localhost:9876/mcp")
			fmt.Println()
			fmt.Printf("Connected APIs: %d\n", len(registry.List()))
			fmt.Println("Run 'musterflow connect <url>' to add an API")
			return nil
		},
	}
}

func newConnectCommand(registry *app.Registry) *cobra.Command {
	var baseURL string
	var nameInput string
	var authType string
	var tolerate bool

	cmd := &cobra.Command{
		Use:   "connect <spec-url>",
		Short: "Connect an API from an OpenAPI spec URL or file path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specURL := args[0]

			// If the dashboard is running, route through its API to avoid lock conflicts.
			if dashboardBaseURL != "" {
				if tolerate {
					// The dashboard's connect endpoint has no tolerance knob, so
					// honoring the flag silently is impossible — refuse instead of
					// pretending the spec was repaired.
					return fmt.Errorf("--tolerate requires a direct connection: stop the dashboard (it is routing connects through %s) and retry", dashboardBaseURL)
				}
				return connectViaDashboard(specURL, baseURL, nameInput, authType)
			}
			// DF-045: connecting writes to the registry, so a locked store must
			// surface the lock instead of a bare "registry not loaded".
			if err := StoreUnavailable(); err != nil {
				return err
			}

			result, err := app.Connect(cmd.Context(), registry, app.ConnectOptions{
				SpecURL:  specURL,
				BaseURL:  baseURL,
				Name:     nameInput,
				AuthType: authType,
				Tolerate: tolerate,
			})
			if err != nil {
				return err
			}

			fmt.Printf("✓ Connected: %s\n", result.SpecTitle)
			fmt.Printf("  ID: %s\n", result.Connection.ID)
			fmt.Printf("  Version: %s\n", result.SpecVersion)
			fmt.Printf("  Endpoints: %d\n", result.EndpointCount)
			fmt.Printf("  Base URL: %s\n", result.Connection.BaseURL)
			if result.Connection.BaseURL == "" {
				// The spec declares no servers[] URL and --base-url was not
				// given: the command tree is built (every operation is
				// visible), but no request can be addressed without a base
				// URL at call time.
				fmt.Printf("  ⚠ No base URL in the spec — pass --base-url here or override per call with --base-url\n")
			}
			printToleratedDefects(result.Tolerated, "connected")
			fmt.Printf("\nTry: musterflow %s --help\n", result.Connection.Name)

			return nil
		},
	}

	cmd.Flags().StringVarP(&baseURL, "base-url", "u", "", "Override base URL for the API")
	cmd.Flags().StringVarP(&nameInput, "name", "n", "", "Human-readable name for the API")
	cmd.Flags().StringVar(&authType, "auth", "none", "Auth type: none, apikey, bearer, oauth2, mtls")
	cmd.Flags().BoolVar(&tolerate, "tolerate", false,
		"Repair cosmetic spec defects (bad examples, boolean required, array without items, dangling refs) and connect in a degraded state; off by default")

	return cmd
}

// printToleratedDefects reports what a tolerant parse repaired or recorded so a
// degraded connection is never silent. Codes are aggregated with counts and a
// handful of representative paths, so a spec with hundreds of defects still
// produces bounded output. verb names the operation that ran the repair pass.
func printToleratedDefects(warnings []app.ToleratedDefect, verb string) {
	if len(warnings) == 0 {
		return
	}
	counts := map[string]int{}
	paths := map[string][]string{}
	order := make([]string, 0, len(warnings))
	for _, w := range warnings {
		if counts[w.Code] == 0 {
			order = append(order, w.Code)
		}
		counts[w.Code]++
		if len(paths[w.Code]) < 3 {
			paths[w.Code] = append(paths[w.Code], w.Path)
		}
	}
	sort.Strings(order)

	fmt.Printf("\n⚠ tolerated %d cosmetic spec defect(s) — %s in a degraded state:\n", len(warnings), verb)
	for _, code := range order {
		fmt.Printf("  %-24s ×%d  (%s)\n", code, counts[code], strings.Join(paths[code], ", "))
	}
}

func newListCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List connected APIs",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dashboardBaseURL != "" {
				return listViaDashboard()
			}
			// DF-045: an unreadable store is not an empty registry. Fail with
			// the cause (and the fix) instead of printing "No APIs connected."
			if err := StoreUnavailable(); err != nil {
				return err
			}
			conns := registry.List()
			if len(conns) == 0 {
				fmt.Println("No APIs connected.")
				fmt.Println("Run 'musterflow connect <url>' to add one.")
				return nil
			}
			fmt.Printf("Connected APIs (%d):\n\n", len(conns))
			for _, conn := range conns {
				fmt.Printf("  %s (%s)\n", conn.Name, conn.ID)
				fmt.Printf("    Spec: %s\n", conn.SpecURL)
				fmt.Printf("    Base: %s\n", conn.BaseURL)
				fmt.Printf("    Endpoints: %d\n", conn.EndpointCount)
				fmt.Printf("    Auth: %s\n", conn.AuthType)
				fmt.Println()
			}
			return nil
		},
	}
}

func newDisconnectCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect <api-id>",
		Short: "Disconnect an API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// If the dashboard is running, route through its API to avoid lock conflicts.
			if dashboardBaseURL != "" {
				return disconnectViaDashboard(args[0])
			}
			// DF-045: a locked store must name the lock, not "registry not loaded".
			if err := StoreUnavailable(); err != nil {
				return err
			}
			if err := app.Disconnect(registry, args[0]); err != nil {
				return err
			}
			fmt.Printf("✓ Disconnected: %s\n", args[0])
			return nil
		},
	}
}

// bundledCatalogNotice is printed when the community index could not be served
// and the catalog bundled inside the binary answers instead. The degradation is
// visible on purpose: the line names both "community index" and "bundled" so it
// is greppable, and it carries the community-side cause when one is known.
func bundledCatalogNotice(w io.Writer, reason error) {
	line := "notice: community index unavailable — serving bundled seed entries (built-in catalog/seed.json)"
	if reason != nil {
		line += ": " + reason.Error()
	}
	_, _ = fmt.Fprintln(w, line)
}

// printCatalogEntries renders catalog entries in the table shape `catalog
// search` has always used, so list and search stay visually consistent.
func printCatalogEntries(w io.Writer, label string, entries []catalog.CatalogEntry) {
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(w, "No catalog entries found.")
		return
	}
	_, _ = fmt.Fprintf(w, "%s (%d):\n\n", label, len(entries))
	_, _ = fmt.Fprintf(w, "  %-20s %-20s %-6s %-60s %s\n", "ID", "NAME", "TYPE", "DESCRIPTION", "DOWNLOADS")
	for _, e := range entries {
		desc := e.Description
		if len(desc) > 60 {
			desc = desc[:60]
		}
		_, _ = fmt.Fprintf(w, "  %-20s %-20s %-6s %-60s %d\n", e.ID, e.Name, e.Type, desc, e.Downloads)
	}
}

// catalogSearch resolves a catalog search, degrading to the bundled seed
// catalog when the community index cannot be served. Both routing modes (local
// and through a running dashboard) answer the same way: the dashboard path only
// changes where the community answer is fetched from, not what happens when
// there isn't one.
func catalogSearch(query string) (catalog.Catalog, error) {
	if dashboardBaseURL != "" {
		entries, err := catalogSearchViaDashboard(query)
		if err == nil {
			return catalog.Catalog{Entries: entries, Source: catalog.SourceCommunity}, nil
		}
		res, fallbackErr := catalog.NewClient().SearchCatalog(query)
		if fallbackErr != nil {
			return catalog.Catalog{}, err
		}
		if res.Source == catalog.SourceBundled {
			res.Reason = err
		}
		return res, nil
	}
	return catalog.NewClient().SearchCatalog(query)
}

func newCatalogCommand(registry *app.Registry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Community catalog operations",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "search <query>",
		Short: "Search the community catalog",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := catalogSearch(args[0])
			if err != nil {
				return err
			}
			if res.Source == catalog.SourceBundled {
				bundledCatalogNotice(cmd.ErrOrStderr(), res.Reason)
			}
			printCatalogEntries(cmd.OutOrStdout(), "Catalog search results", res.Entries)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List every entry in the catalog",
		Long: "List every catalog entry. When the community index cannot be served the\n" +
			"binary's bundled seed entries are listed instead, with a notice saying so.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// No dashboard equivalent: listing is a pure read, so it always
			// goes to the catalog index directly.
			res, err := catalog.NewClient().ListCatalog()
			if err != nil {
				return err
			}
			if res.Source == catalog.SourceBundled {
				bundledCatalogNotice(cmd.ErrOrStderr(), res.Reason)
			}
			printCatalogEntries(cmd.OutOrStdout(), "Catalog entries", res.Entries)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "push <api-id|name>",
		Short: "Push a connected API to the community catalog",
		Long: "Print the community-catalog entry JSON for a connected API.\n" +
			"The API may be given by id (as shown by 'musterflow list') or by its name.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dashboardBaseURL != "" {
				return pushViaDashboard(args[0])
			}
			// DF-045: registry lookups on an unreadable store must name the
			// lock, not report the API as missing.
			if err := StoreUnavailable(); err != nil {
				return err
			}
			// DF-024: resolve name-or-id, surfacing the lookup error unchanged
			// so it names the connected APIs.
			conn, err := registry.GetByNameOrID(args[0])
			if err != nil {
				return err
			}
			entry := catalog.ConnectionToCatalogEntry(conn)
			data, err := entry.ToJSON()
			if err != nil {
				return fmt.Errorf("marshal entry: %w", err)
			}
			fmt.Printf("Catalog entry JSON for %s:\n\n", conn.ID)
			fmt.Println(string(data))
			fmt.Println()
			fmt.Printf("Submit a PR to https://github.com/totalwindupflightsystems/musterflow-catalog with the entry JSON at entries/%s.json and add it to entries.json\n", conn.ID)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "pull <api-id>",
		Short: "Pull an API from the community catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dashboardBaseURL != "" {
				return pullViaDashboard(cmd, args[0])
			}
			// DF-045: pulling writes to the registry; name the lock up front.
			if err := StoreUnavailable(); err != nil {
				return err
			}
			// CATALOG-006: resolve the entry from the community index, or from
			// the bundled seed catalog when that index cannot be served.
			res, err := catalog.NewClient().FetchCatalogEntry(args[0])
			if err != nil {
				return err
			}
			entry := res.Entry
			if entry == nil {
				fmt.Printf("Entry %s not found in catalog.\n", args[0])
				return nil
			}
			if res.Source == catalog.SourceBundled {
				bundledCatalogNotice(cmd.ErrOrStderr(), res.Reason)
			}
			fmt.Printf("Pulling %s (%s) from community catalog...\n", entry.ID, entry.Name)
			specData, err := loadSpecData(entry.SpecURL)
			if err != nil {
				return fmt.Errorf("download spec: %w", err)
			}
			result, err := app.Connect(cmd.Context(), registry, app.ConnectOptions{
				SpecURL: entry.SpecURL,
				Name:    entry.Name,
			})
			if err != nil {
				return fmt.Errorf("connect: %w", err)
			}
			fmt.Printf("✓ Pulled and connected: %s\n", result.SpecTitle)
			fmt.Printf("  ID: %s\n", result.Connection.ID)
			fmt.Printf("  Endpoints: %d\n", result.EndpointCount)
			fmt.Printf("  Spec data: %d bytes\n", len(specData))
			return nil
		},
	})

	return cmd
}

// defaultFlowSource is the Starlark template written by `flow create` when no
// --source is supplied. It is a single source of truth for both create paths:
// the local engine path here and the dashboard payload path in register.go.
//
// The template must run as-is: `musterflow flow run <name>` on a freshly
// created flow prints "Hello from flow" and exits 0. It also demonstrates the
// trigger global, which is a dict for webhook/dashboard payloads and None for
// a plain `flow run`.
//
// The shape is dictated by the Starlark dialect, verified empirically against
// this engine: top-level `if`/`for` STATEMENTS do not compile ("if statement
// not within a function"), so trigger handling lives inside a def that is
// called at the end. Referencing `trigger` in a top-level EXPRESSION (e.g.
// inside a print(...) call) does compile, but a def is the reliable place for
// any branching logic.
const defaultFlowSource = `# MusterFlow Starlark workflow - runs as-is: musterflow flow run <name>
#
# Globals: trigger (payload dict, or None on a plain run), print, and the HTTP
# helpers http_get(url), http_post(url, body), http_request(method, url) -
# each returns {"status", "body", "headers"}.
# Top-level code may only be assignments and defs: if/for STATEMENTS must live
# inside a def (a top-level "if trigger != None:" fails to compile).
def main():
    if trigger == None:
        print("Hello from flow")
    else:
        print("Triggered with payload: " + str(trigger))

main()
`

func newFlowCommand(registry *app.Registry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flow",
		Short: "Workflow management",
	}

	var (
		webhook     bool
		description string
		source      string
		name        string
		runName     string
		payload     string
	)

	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new workflow",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flowName := name
			if flowName != "" {
				if len(args) == 1 && args[0] != flowName {
					return fmt.Errorf("conflicting flow names: positional %q vs --name %q", args[0], flowName)
				}
			} else if len(args) == 1 {
				flowName = args[0]
			} else {
				return fmt.Errorf("flow name required (positional or --name)")
			}
			flowSource := source
			if flowSource == "" {
				flowSource = defaultFlowSource
			}
			if dashboardBaseURL != "" {
				return flowCreateViaDashboard(flowName, flowSource, description, webhook)
			}
			engine := workflow.NewEngine(
				filepath.Join(registry.DataDir(), "flows"),
				"http://localhost:9876",
			)
			flow, err := engine.Create(flowName, flowSource, description, webhook)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Created flow %q\n", flow.Name)
			if flow.Description != "" {
				fmt.Printf("  Description: %s\n", flow.Description)
			}
			if webhook {
				fmt.Printf("  Webhook URL: %s\n", flow.WebhookURL)
			}
			// DF-028: only print the Edit hint when the .star file actually
			// exists on disk, so we never tell the user to edit a file that
			// isn't there.
			starPath := filepath.Join(registry.DataDir(), "flows", flow.Name+".star")
			if _, statErr := os.Stat(starPath); statErr == nil {
				fmt.Printf("  Edit: %s/flows/%s.star\n", registry.DataDir(), flow.Name)
			}
			return nil
		},
	}
	createCmd.Flags().BoolVar(&webhook, "webhook", false, "Create a webhook trigger for this flow")
	createCmd.Flags().StringVar(&description, "description", "", "Flow description")
	createCmd.Flags().StringVar(&source, "source", "", "Starlark source code for the flow")
	createCmd.Flags().StringVar(&name, "name", "", "Flow name (alternative to positional argument)")
	cmd.AddCommand(createCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dashboardBaseURL != "" {
				return flowListViaDashboard()
			}
			engine := workflow.NewEngine(
				filepath.Join(registry.DataDir(), "flows"),
				"http://localhost:9876",
			)
			flows, err := engine.List()
			if err != nil {
				return err
			}
			if len(flows) == 0 {
				fmt.Println("No workflows defined.")
				fmt.Println("Create one with: musterflow flow create <name>")
				return nil
			}
			fmt.Println("Workflows:")
			for _, f := range flows {
				fmt.Printf("  %s", f.Name)
				if f.Description != "" {
					fmt.Printf("  - %s", f.Description)
				}
				if f.WebhookURL != "" {
					fmt.Printf("  webhook: %s", f.WebhookURL)
				}
				fmt.Println()
			}
			return nil
		},
	})

	runCmd := &cobra.Command{
		Use:   "run [name]",
		Short: "Run a workflow",
		Long:  "Run a workflow.\nThe flow name may be given positionally (`flow run <name>`) or with --name.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// DF-034: accept --name as well as the positional form, using the
			// same resolution rules as `flow create` so sibling subcommands
			// expose one flag shape. The flag variable is per-command
			// (runName, not the shared `name`) so a value parsed for
			// `flow create --name X` can never leak into a later
			// `flow run` in the same process.
			flowName := runName
			if flowName != "" {
				if len(args) == 1 && args[0] != flowName {
					return fmt.Errorf("conflicting flow names: positional %q vs --name %q", args[0], flowName)
				}
			} else if len(args) == 1 {
				flowName = args[0]
			} else {
				return fmt.Errorf("flow name required (positional or --name)")
			}
			var triggerPayload map[string]interface{}
			if payload != "" {
				if err := json.Unmarshal([]byte(payload), &triggerPayload); err != nil {
					return fmt.Errorf("parse --payload: %w", err)
				}
			}
			if dashboardBaseURL != "" {
				return flowRunViaDashboard(flowName, triggerPayload)
			}
			engine := workflow.NewEngine(
				filepath.Join(registry.DataDir(), "flows"),
				"http://localhost:9876",
			)
			output, err := engine.Run(flowName, triggerPayload)
			if err != nil {
				return err
			}
			fmt.Print(output)
			if !strings.HasSuffix(output, "\n") {
				fmt.Println()
			}
			return nil
		},
	}
	runCmd.Flags().StringVar(&payload, "payload", "", "JSON trigger payload for the flow")
	runCmd.Flags().StringVar(&runName, "name", "", "Flow name (alternative to positional argument)")
	cmd.AddCommand(runCmd)

	return cmd
}

func newMCPCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "MCP endpoint information",
		RunE: func(cmd *cobra.Command, args []string) error {
			// If the dashboard is running, route through its API to avoid lock conflicts.
			if dashboardBaseURL != "" {
				return mcpViaDashboard()
			}
			// DF-045: an unreadable store is not an empty registry.
			if err := StoreUnavailable(); err != nil {
				return err
			}
			fmt.Println("MCP endpoint: http://localhost:9876/mcp")
			fmt.Println("Transport: HTTP JSON-RPC 2.0")
			fmt.Println()
			conns := registry.List()
			if len(conns) == 0 {
				fmt.Println("No APIs connected. Connect APIs to expose them as MCP tools.")
				return nil
			}
			fmt.Printf("Exposed MCP tools from %d APIs:\n\n", len(conns))
			for _, conn := range conns {
				fmt.Printf("  [%s] %s — %d tools\n", conn.Name, conn.Description, conn.EndpointCount)
			}
			fmt.Println("\nConnect an MCP client to http://localhost:9876/mcp to use these tools.")
			return nil
		},
	}
}

func newConfigCommand(registry *app.Registry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage MusterFlow configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			// Mask auth keys and the dashboard token — the same helper.
			for id, auth := range cfg.Auth {
				auth.Key = config.MaskKey(auth.Key)
				cfg.Auth[id] = auth
			}
			fmt.Printf("Port:              %d\n", cfg.Port)
			fmt.Printf("Data directory:    %s\n", cfg.DataDir)
			fmt.Printf("Default format:    %s\n", cfg.DefaultFormat)
			fmt.Printf("Auto-completion:   %v\n", cfg.AutoCompletion)
			if cfg.Timeout > 0 {
				fmt.Printf("Timeout:           %d\n", cfg.Timeout)
			} else {
				fmt.Printf("Timeout:           (unset — engine default: 30s per attempt, up to 3 attempts)\n")
			}
			if cfg.DashboardToken != "" {
				fmt.Printf("Dashboard token:   %s\n", config.MaskKey(cfg.DashboardToken))
			} else {
				fmt.Printf("Dashboard token:   (not set — dashboard routes are unauthenticated)\n")
			}
			fmt.Printf("Config file:       %s\n", configPath())
			if len(cfg.Auth) > 0 {
				fmt.Println("\nAuth:")
				for id, auth := range cfg.Auth {
					fmt.Printf("  %s: type=%s key=%s\n", id, auth.Type, auth.Key)
				}
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			key, val := args[0], args[1]
			switch key {
			case "port":
				_, _ = fmt.Sscanf(val, "%d", &cfg.Port)
			case "default_format":
				cfg.DefaultFormat = val
			case "auto_completion":
				cfg.AutoCompletion = val == "true" || val == "1" || val == "yes"
			case "data_dir":
				cfg.DataDir = val
			case "dashboard_token":
				cfg.SetDashboardToken(val)
			case "timeout":
				secs, err := config.ParseTimeoutSeconds(val)
				if err != nil {
					return err
				}
				cfg.Timeout = secs
			default:
				return fmt.Errorf("unknown config key: %s (valid: port, default_format, auto_completion, data_dir, dashboard_token, timeout)", key)
			}
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			if key == "dashboard_token" {
				// Never echo the secret back to the terminal / shell history.
				fmt.Printf("✓ Set %s = %s\n", key, config.MaskKey(val))
				return nil
			}
			fmt.Printf("✓ Set %s = %s\n", key, val)
			return nil
		},
	})

	return cmd
}

func newAuthCommand(registry *app.Registry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage API credentials",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <api-id>",
		Short: "Add credentials for an API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			mgr := auth.NewManager(cfg)

			authType := auth.CredentialTypeFromString(typeFlag)
			cred := auth.Credential{
				Type:     authType,
				Key:      keyFlag,
				CertPath: certFlag,
				KeyPath:  keyPathFlag,
			}
			if err := mgr.Add(args[0], cred); err != nil {
				return fmt.Errorf("add auth: %w", err)
			}
			fmt.Printf("✓ Added %s auth for %s\n", authType, args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured credentials (keys masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			mgr := auth.NewManager(cfg)
			entries := mgr.List()
			if len(entries) == 0 {
				fmt.Println("No credentials configured.")
				fmt.Println("Use 'musterflow auth add <api-id> --type apikey --key sk-xxx' to add one.")
				return nil
			}
			fmt.Println("Configured credentials:")
			for _, e := range entries {
				fmt.Printf("  %s: type=%s key=%s", e.APIID, e.Type, e.Key)
				if e.CertPath != "" {
					fmt.Printf(" cert=%s", e.CertPath)
				}
				fmt.Println()
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <api-id>",
		Short: "Remove credentials for an API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			mgr := auth.NewManager(cfg)
			if err := mgr.Remove(args[0]); err != nil {
				return err
			}
			// Drop any copy exported to the engine keychain for `--auth`
			// (best effort: a headless host may have no usable keychain).
			_ = auth.PurgeEngineCredential(args[0])
			fmt.Printf("✓ Removed auth for %s\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <api-id>",
		Short: "Get the credential value for an API (prints raw key)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			mgr := auth.NewManager(cfg)
			cred, err := mgr.Get(args[0])
			if err != nil {
				return err
			}
			fmt.Print(cred.Key)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "login <api-id>",
		Short: "Start OAuth2 authorization code flow for an API",
		Long: `Start an OAuth2 authorization code flow. Opens a browser to the
authorization URL, starts a local callback server, exchanges the
code for a token, and stores the credential.

Flags --client-id, --client-secret, --auth-url, and --token-url
are required for the OAuth2 flow.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			apiID := args[0]

			// Validate required OAuth2 flags
			if oauthClientID == "" {
				return fmt.Errorf("--client-id is required for OAuth2 login")
			}
			if oauthClientSecret == "" {
				return fmt.Errorf("--client-secret is required for OAuth2 login")
			}
			if oauthAuthURL == "" {
				return fmt.Errorf("--auth-url is required for OAuth2 login")
			}
			if oauthTokenURL == "" {
				return fmt.Errorf("--token-url is required for OAuth2 login")
			}
			if oauthRedirectPort == 0 {
				oauthRedirectPort = 19876
			}

			fmt.Printf("Starting OAuth2 flow for %s\n", apiID)
			fmt.Printf("  Auth URL: %s\n", oauthAuthURL)
			fmt.Printf("  Token URL: %s\n", oauthTokenURL)
			fmt.Println()

			cfg := auth.OAuth2Config{
				ClientID:     oauthClientID,
				ClientSecret: oauthClientSecret,
				AuthURL:      oauthAuthURL,
				TokenURL:     oauthTokenURL,
				Scopes:       oauthScopes,
				RedirectPort: oauthRedirectPort,
			}

			authReq, err := auth.StartLogin(cfg)
			if err != nil {
				return fmt.Errorf("start OAuth2 flow: %w", err)
			}

			// Open browser
			fmt.Printf("Opening browser to: %s\n", authReq.URL)
			if err := auth.OpenBrowser(authReq.URL); err != nil {
				fmt.Fprintf(os.Stderr, "Could not open browser: %v\n", err)
				fmt.Fprintf(os.Stderr, "Please open this URL manually:\n  %s\n", authReq.URL)
			}

			// Start callback server
			fmt.Printf("\nWaiting for authorization (listening on port %d)...\n", oauthRedirectPort)
			fmt.Println("Complete authorization in your browser, then return here.")

			code, err := startCallbackServer(oauthRedirectPort)
			if err != nil {
				return fmt.Errorf("callback server: %w", err)
			}

			fmt.Println("\nAuthorization received. Exchanging code for token...")

			store := auth.NewYAMLTokenStore(filepath.Join(app.DefaultDataDir(), "tokens"))
			token, err := auth.CompleteLogin(store, cfg, authReq, code)
			if err != nil {
				return fmt.Errorf("complete login: %w", err)
			}

			// Store credential via auth manager
			cfg2, err := loadConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			mgr := auth.NewManager(cfg2)
			cred := auth.Credential{
				Type: auth.CredentialOAuth2,
				Key:  token.AccessToken,
			}
			if err := mgr.Add(apiID, cred); err != nil {
				return fmt.Errorf("store credential: %w", err)
			}

			fmt.Printf("✓ OAuth2 login complete for %s\n", apiID)
			fmt.Printf("  Token type: %s\n", token.TokenType)
			if token.RefreshToken != "" {
				fmt.Printf("  Refresh token: stored\n")
			}
			fmt.Printf("  Expires: %s\n", token.ExpiresAt.Format(time.RFC3339))
			return nil
		},
	})

	// OAuth2 login flags
	cmd.PersistentFlags().StringVar(&oauthClientID, "client-id", "", "OAuth2 client ID")
	cmd.PersistentFlags().StringVar(&oauthClientSecret, "client-secret", "", "OAuth2 client secret")
	cmd.PersistentFlags().StringVar(&oauthAuthURL, "auth-url", "", "OAuth2 authorization URL")
	cmd.PersistentFlags().StringVar(&oauthTokenURL, "token-url", "", "OAuth2 token URL")
	cmd.PersistentFlags().StringSliceVar(&oauthScopes, "scopes", nil, "OAuth2 scopes (comma-separated)")
	cmd.PersistentFlags().IntVar(&oauthRedirectPort, "redirect-port", 19876, "Local callback server port")

	// Flags for the 'add' subcommand — scoped to the auth command
	cmd.PersistentFlags().StringVar(&typeFlag, "type", "bearer", "Auth type: none, apikey, bearer, oauth2, mtls")
	cmd.PersistentFlags().StringVar(&keyFlag, "key", "", "API key or bearer token")
	cmd.PersistentFlags().StringVar(&certFlag, "cert", "", "mTLS client certificate path")
	cmd.PersistentFlags().StringVar(&keyPathFlag, "key-path", "", "mTLS client key path")

	return cmd
}
