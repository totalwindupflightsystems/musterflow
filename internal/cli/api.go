// Package cli provides API subcommand loading and spec-parsing infrastructure.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/openapi"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// apiCommandState tracks lazy generation of cobra commands for a connected API.
// Commands are generated on first access via sync.Once so startup stays fast.
type apiCommandState struct {
	once sync.Once
	err  error
}

var apiCommands = make(map[string]*apiCommandState)
var apiCommandsMu sync.Mutex

// loadAPISubcommands registers a cobra subcommand for every connected API.
// When dashboardBaseURL is set (the dashboard process is running and holds
// the DuckDB write lock, so registry.List() is empty) the connection list is
// fetched from GET <dashboardBaseURL>/api/apis instead.  The fetched
// APIConnection values are identical to what registry.List() returns — they
// round-trip through the same JSON tags — so createAPISubcommand and the
// lazy-load flow (ensureAPILoaded) work unchanged.  If the fetch fails
// (dashboard died between detection and command build) the function logs a
// warning to stderr and falls back to registry.List(); if the registry is also
// empty, no API commands are registered (matching the pre-existing behavior).
// A dashboard AUTH rejection (HTTP 401/403, DF-052) is the exception: no
// fallback runs and the rejection is recorded for ResolveExecutionError so
// every failing invocation names authentication instead of the command tree.
//
// dataDir is where each connection's cached command plan lives (PERF-002).
func loadAPISubcommands(root *cobra.Command, registry *app.Registry) {
	conns := registry.List()
	if dashboardBaseURL != "" {
		fetched, err := fetchAPIsFromDashboard()
		if err != nil {
			if isDashboardAuthError(err) {
				// DF-052: a tokened dashboard rejecting the CLI (HTTP
				// 401/403) is not a transient fetch failure, so the local
				// registry fallback must not run — in the routed mode that
				// fallback is empty anyway (the dashboard holds the DuckDB
				// lock) and every generated command would degrade into
				// `unknown command "<api>"`, hiding the auth cause. The
				// rejection is recorded (dashboardAuthError sets it), so
				// main's ResolveExecutionError turns that unknown-command
				// shape into the auth message, and listViaDashboard fails
				// with the same text.
				return
			}
			fmt.Fprintf(os.Stderr, "Warning: could not fetch APIs from dashboard (%s): %v; falling back to local registry\n", dashboardBaseURL, err)
		} else {
			conns = fetched
		}
	}
	dataDir := registry.DataDir()
	for _, conn := range conns {
		apiCmd := createAPISubcommand(conn, dataDir)
		root.AddCommand(apiCmd)
	}
}

// fetchAPIsFromDashboard fetches the connected API list from the running
// dashboard's GET /api/apis endpoint.  The response body is an object with an
// "apis" key whose value is a JSON array of APIConnection — mirroring the
// pattern used by listViaDashboard (register.go).
func fetchAPIsFromDashboard() ([]*app.APIConnection, error) {
	resp, err := http.Get(dashboardBaseURL + "/api/apis")
	if err != nil {
		return nil, fmt.Errorf("dashboard apis request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, dashboardStatusError(resp.StatusCode)
	}

	var result struct {
		APIs []*app.APIConnection `json:"apis"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode dashboard response: %w", err)
	}
	return result.APIs, nil
}

// loadSpecData fetches a spec from a URL or reads it from a local file path.
func loadSpecData(specURL string) ([]byte, error) {
	if strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://") {
		resp, err := http.Get(specURL)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(specURL)
}

// createAPISubcommand creates a cobra command that lazily loads subcommands
// for the given API connection. dataDir is the store holding the connection's
// cached command plan.
func createAPISubcommand(conn *app.APIConnection, dataDir string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   conn.Name,
		Short: fmt.Sprintf("Commands for %s API", conn.Name),
		Long:  conn.Description,
		// Normal flag parsing (replaces the old DisableFlagParsing: true).
		// Root persistent flags (--data-dir, --dashboard-addr, --output-file)
		// and --help are inherited via cobra's persistent-flag mechanism and
		// parsed correctly on the group.  Unknown flags cause a parse error
		// (exit non-zero, DF-007 preserved).  Leaf-specific flags appear
		// after the first positional arg (resource/op); SetInterspersed(false)
		// makes pflag stop parsing at the first non-flag arg so leaf flags
		// flow into RunE → Find → target re-execution intact (DF-001/DF-002
		// preserved).  See DF-011.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return ensureAPILoaded(cmd, conn, dataDir, focusOf(args))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureAPILoaded(cmd, conn, dataDir, focusOf(args)); err != nil {
				return err
			}
			if len(args) == 0 {
				return cmd.Help()
			}
			// Subcommands are now loaded. Find the target subcommand.
			target, remaining, e := cmd.Find(args)
			if e != nil {
				return e
			}
			if target == nil || target == cmd {
				// Unknown subcommand under an API parent — return an
				// error so the process exits non-zero (DF-007).
				return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
			}
			// Re-execute through cobra's pipeline starting at the target.
			// target is NOT our API command (which has normal flag parsing),
			// so this won't recurse — target handles its own flag parsing.
			return executeLeaf(target, remaining)
		},
	}
	// Disable interspersed parsing so pflag stops at the first positional
	// arg (resource/op).  This preserves leaf-specific flags that appear
	// after the positional args — they flow into RunE → Find → target
	// re-execution intact (DF-001/DF-002).  Without this, pflag would try
	// to parse leaf flags (--limit, --tags, etc.) on the group command and
	// fail with "unknown flag".  See DF-011.
	cmd.Flags().SetInterspersed(false)
	// Override help so lazy-loaded subcommands appear in --help output.
	// cobra resolves the command tree before PersistentPreRunE fires, so we
	// trigger the lazy load from the help func too.
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		// DF-048: a help request must never show an empty command list for a
		// connection that cannot load. Report the load failure instead of
		// printing a usage block with no commands under it.
		if err := ensureAPILoaded(c, conn, dataDir, ""); err != nil {
			_, _ = fmt.Fprintf(c.ErrOrStderr(), "Error: %v\n", err)
			return
		}
		// If args reference a subcommand, find and show its help.
		searchArgs := args
		for len(searchArgs) > 0 && searchArgs[0] == c.Name() {
			searchArgs = searchArgs[1:]
		}
		if len(searchArgs) > 0 {
			target, _, e := c.Find(searchArgs)
			if e == nil && target != nil && target != c {
				_, _ = fmt.Fprint(target.OutOrStdout(), target.UsageString())
				return
			}
		}
		_, _ = fmt.Fprint(c.OutOrStdout(), c.UsageString())
	})
	// ValidArgsFunction provides dynamic completion for lazily-generated
	// API subcommands. Cobra's V2 bash completion (and built-in fish/zsh
	// completion) calls the binary with __complete at completion time,
	// which triggers this function to load and enumerate subcommands.
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if err := ensureAPILoaded(cmd, conn, dataDir, ""); err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, sub := range cmd.Commands() {
			names = append(names, sub.Name())
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}

// ensureAPILoaded lazily generates cobra commands for a connected API on first
// access via sync.Once. Safe to call from multiple paths (PersistentPreRunE,
// help func). Subsequent calls are no-ops.
func ensureAPILoaded(cmd *cobra.Command, conn *app.APIConnection, dataDir, focus string) error {
	connID := conn.ID
	apiCommandsMu.Lock()
	state, exists := apiCommands[connID]
	if !exists {
		state = &apiCommandState{}
		apiCommands[connID] = state
	}
	apiCommandsMu.Unlock()

	state.once.Do(func() {
		state.err = loadAPICommands(cmd, conn, dataDir, focus)
	})
	return state.err
}

// focusOf names the resource an invocation targets: the first positional
// argument after the API name (e.g. "items" in `musterflow vendor items
// list-items`). An empty focus means "no single resource is being invoked"
// (bare `--help`, completion, or an argument-less invocation), which needs the
// full command tree to describe.
func focusOf(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if strings.HasPrefix(args[0], "-") {
		return ""
	}
	return args[0]
}

// loadAPICommands attaches the generated subcommands for one connection.
//
// The command plan cached at connect time is the source of truth (PERF-002):
// an operation call loads a small JSON plan from the data dir and never reads
// the source spec or its URL, so cost does not scale with spec size and a
// connected API keeps working after its source file moves away.
//
// A cache miss (a connection made before the plan cache existed, a plan
// deleted by hand, or a data dir moved between hosts) falls back to the source
// spec exactly once and rewrites the plan.
func loadAPICommands(parent *cobra.Command, conn *app.APIConnection, dataDir, focus string) error {
	idx, err := app.ReadPlanIndex(dataDir, conn)
	if err != nil {
		plan, rerr := rebuildPlan(conn, dataDir)
		if rerr != nil {
			return rerr
		}
		// The rebuilt plan could not be cached (e.g. a read-only data dir):
		// build the whole tree from it in memory. Correct, just not lazy.
		return attachFullPlan(parent, plan, conn)
	}
	return attachPlanCommands(parent, idx, conn, dataDir, focus)
}

// attachFullPlan adds the whole generated command tree for an in-memory plan.
func attachFullPlan(parent *cobra.Command, plan *app.SpecPlan, conn *app.APIConnection) error {
	commands, err := app.CommandsFromPlan(plan, conn)
	if err != nil {
		return fmt.Errorf("%s: build commands from command plan: %w", conn.Name, err)
	}
	if len(commands) == 0 {
		// Never let an API exist with an empty command list: DF-048's whole
		// point is that "connected" must mean "callable".
		return app.DiagnoseEmptyPlan(plan)
	}
	for _, cmd := range commands {
		bridgeGeneratedAuth(cmd)
		parent.AddCommand(cmd)
	}
	return nil
}

// attachPlanCommands adds the API's resource groups to parent.
//
// With an indexed plan and a named resource, only that resource's operation
// commands are built; every other resource becomes a lazy group that loads its
// own operations the first time it is used. That keeps an operation call's cost
// tied to the invoked resource instead of to the size of the whole API. Without
// an index (or without a focused resource) the whole tree is built, which is
// the historical behaviour.
func attachPlanCommands(parent *cobra.Command, idx *app.PlanIndex, conn *app.APIConnection, dataDir, focus string) error {
	if focus == "" || len(idx.Resources()) == 0 {
		ops, err := idx.AllOperations(dataDir, conn)
		if err != nil {
			return fmt.Errorf("%s: read cached command plan: %w", conn.Name, err)
		}
		return attachFullPlan(parent, idx.Plan(ops), conn)
	}

	focused := false
	if idx.HasResource(focus) {
		ops, err := idx.ResourceOperations(dataDir, conn, focus)
		if err != nil {
			return fmt.Errorf("%s %s: read cached command plan: %w", conn.Name, focus, err)
		}
		commands, err := app.CommandsFromPlan(idx.Plan(ops), conn)
		if err != nil {
			return fmt.Errorf("%s %s: build commands from cached spec plan: %w", conn.Name, focus, err)
		}
		for _, cmd := range commands {
			bridgeGeneratedAuth(cmd)
			parent.AddCommand(cmd)
		}
		focused = len(commands) > 0
	}

	for _, res := range idx.Resources() {
		if focused && res.Name == focus {
			continue
		}
		parent.AddCommand(newLazyResourceGroup(res, idx, conn, dataDir))
	}
	return nil
}

// newLazyResourceGroup builds a resource group whose operation commands are
// generated on first use. The group's name and description come from the plan's
// index (derived from the generator's own output), so `--help` and completion
// show the same resources as a fully generated tree, while a call into any one
// resource only builds that resource's leaves.
func newLazyResourceGroup(res app.PlanResource, idx *app.PlanIndex, conn *app.APIConnection, dataDir string) *cobra.Command {
	group := &cobra.Command{Use: res.Name, Short: res.Short}
	// The generator gives every resource group this runtime override flag and
	// its leaves read it through inherited flags; the lazy group must expose
	// the same flag or leaf lookups fail.
	group.PersistentFlags().String("base-url", "", "Override the API base URL at runtime (env: MUSTER_BASE_URL)")

	var once sync.Once
	var loadErr error
	load := func() error {
		once.Do(func() {
			ops, err := idx.ResourceOperations(dataDir, conn, res.Name)
			if err != nil {
				loadErr = fmt.Errorf("%s %s: read cached command plan: %w", conn.Name, res.Name, err)
				return
			}
			commands, err := app.CommandsFromPlan(idx.Plan(ops), conn)
			if err != nil {
				loadErr = fmt.Errorf("%s %s: build commands: %w", conn.Name, res.Name, err)
				return
			}
			if len(commands) == 0 {
				loadErr = app.DiagnoseEmptyPlan(idx.Plan(nil))
				return
			}
			for _, real := range commands {
				// bridgeGeneratedAuth recurses into the leaves it is given.
				bridgeGeneratedAuth(real)
				for _, leaf := range real.Commands() {
					group.AddCommand(leaf)
				}
			}
		})
		return loadErr
	}

	group.RunE = func(c *cobra.Command, args []string) error {
		if err := load(); err != nil {
			return err
		}
		if len(args) == 0 {
			return c.Help()
		}
		target, remaining, err := c.Find(args)
		if err != nil {
			return err
		}
		if target == nil || target == c {
			return fmt.Errorf("unknown command %q for %q", args[0], c.CommandPath())
		}
		return executeLeaf(target, remaining)
	}
	group.SetHelpFunc(func(c *cobra.Command, args []string) {
		if err := load(); err != nil {
			_, _ = fmt.Fprintf(c.ErrOrStderr(), "Error: %v\n", err)
			return
		}
		_, _ = fmt.Fprint(c.OutOrStdout(), c.UsageString())
	})
	group.ValidArgsFunction = func(c *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if err := load(); err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var names []string
		for _, sub := range c.Commands() {
			names = append(names, sub.Name())
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
	return group
}

// executeLeaf runs one generated operation command with the arguments that
// follow it, applying the output-format interception the CLI owns (DF-020).
//
// PERF-001: before the leaf executes, arm the whole-call operation watchdog
// (when the timeout config key is set) — the engine's generated RunE captures
// its own context.Background(), so this process-level ceiling is the only
// musterflow-owned way to bound a stalled call on this path.
func executeLeaf(target *cobra.Command, remaining []string) error {
	// Silence usage/errors on the target so it doesn't double-print (the root
	// already has SilenceUsage/SilenceErrors; the error propagates back through
	// the parent's RunE to root's ExecuteContext).
	target.SilenceUsage = true
	target.SilenceErrors = true
	// Pre-parse the leaf arguments so the inherited --retries flag is readable
	// before the DF-044 pin decision runs: the engine resolves the attempt
	// ceiling from MUSTER_MAX_RETRIES inside the HTTP call, which happens
	// before target.Execute's own flag parsing. Errors are ignored here — the
	// real parse (and its error reporting) is target.Execute's, and the
	// interception path may re-parse its own rewritten args.
	target.SetArgs(remaining)
	_ = target.ParseFlags(remaining)
	if stop := armOperationWatchdog(target); stop != nil {
		defer stop()
	}
	// DF-044: run the leaf under the write-retry pin — the engine reads
	// MUSTER_MAX_RETRIES per request and replays POST/PATCH whenever the
	// ceiling is above one attempt, so the pin (which honors the inherited
	// --retries opt-in) wraps both interception paths and the direct run.
	return executeLeafUnderRetryPin(target, func() error {
		intercepted, err := interceptOutput(target, remaining)
		if intercepted {
			return err
		}
		target.SetArgs(remaining)
		return target.Execute()
	})
}

// rebuildPlan re-reads and re-parses the connection's source spec, under the
// tolerance opt-in the connection was registered with, derives a plan for it,
// indexes it and rewrites the cache. It is the cache-miss path only.
func rebuildPlan(conn *app.APIConnection, dataDir string) (*app.SpecPlan, error) {
	data, err := loadSpecData(conn.SpecURL)
	if err != nil {
		return nil, fmt.Errorf("no cached command plan for %s and the source spec is unreadable (%w) — re-run 'musterflow connect %s' if the spec moved",
			conn.Name, err, conn.SpecURL)
	}

	fixed, tolerated, repaired, err := app.ApplyTolerance(data, conn.Tolerate)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	parser := openapi.NewParser()
	result, err := parser.Parse(ctx, fixed, openapi.DefaultParseOptions())
	if err != nil {
		defect := app.DiagnoseLoadFailure(fixed)
		if defect == nil {
			defect = &app.LoadDefect{Reason: "document was rejected by the parser"}
		}
		defect.Spec = conn.SpecURL
		defect.Cause = err
		return nil, defect
	}
	if len(tolerated) > 0 {
		fmt.Fprintf(os.Stderr, "⚠ %s: %d cosmetic spec defect(s) tolerated while generating commands\n", conn.Name, len(tolerated))
	}

	plan, err := app.BuildPlan(result.Document, tolerated, repaired)
	if err != nil {
		return nil, fmt.Errorf("%s: derive command plan: %w", conn.Name, err)
	}
	plan.ConnectionID = conn.ID
	plan.SpecURL = conn.SpecURL
	plan.Title = conn.Name
	plan.BaseURL = conn.BaseURL
	plan.ContentSHA256 = app.ContentHash(fixed)

	// Same loadability rule connect applies: a plan that produces no command
	// tree is refused rather than cached.
	commands, err := app.CommandsFromPlan(plan, conn)
	if err != nil {
		return nil, fmt.Errorf("%s: build commands: %w", conn.Name, err)
	}
	if len(commands) == 0 {
		return nil, app.DiagnoseEmptyPlan(plan)
	}
	app.IndexPlanResources(plan, commands)

	// Best effort: a read-only data dir still gets working commands, it just
	// keeps falling back to the source on every call.
	_ = app.WritePlan(dataDir, plan)
	return plan, nil
}
