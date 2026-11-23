// MusterFlow — turn any API into a CLI, an MCP tool, and a workflow.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/auth"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
	"github.com/totalwindupflightsystems/musterflow/internal/cli"
	"github.com/totalwindupflightsystems/musterflow/internal/completion"
	"github.com/totalwindupflightsystems/musterflow/internal/config"
	"github.com/totalwindupflightsystems/musterflow/internal/dashboard"
	"github.com/totalwindupflightsystems/musterflow/internal/mcp"
	"github.com/wojons/muster/pkg/mcp/handlers"
)

var (
	Version   = "0.1.0"
	Commit    = "unknown"
	BuildDate = "unknown"

	// flagDashboardServingMain is flipped by startServer before it sits in
	// its signal wait: the signal goroutine must not hard-exit a serving
	// dashboard (startServer owns the graceful shutdown there).
	dashboardServing atomic.Bool

	// CLI flag overrides
	flagDashboardAddr string
	flagDataDir       string
	flagNoDashboard   bool
)

// applyTimeoutEnv translates the config timeout key into the engine's
// per-attempt MUSTER_HTTP_TIMEOUT knob. Only when the key is set (Timeout >
// 0) AND the operator did not export the var themselves — explicit
// MUSTER_HTTP_TIMEOUT keeps precedence over the config file (the docs state
// this: the env var is the operator override).
func applyTimeoutEnv(cfg config.Config) {
	if cfg.Timeout <= 0 {
		return
	}
	if os.Getenv(config.TimeoutEnvVar) != "" {
		return
	}
	_ = os.Setenv(config.TimeoutEnvVar, strconv.Itoa(cfg.Timeout))
}

// signalAbortMessage is the one-shot command's stderr line on SIGINT/SIGTERM.
// os.Signal.String() prints "interrupt"/"terminated"; the canonical signal
// names are what an operator grep-reads, so map the two supported ones.
func signalAbortMessage(sig os.Signal) string {
	name := sig.String()
	switch sig {
	case syscall.SIGINT:
		name = "SIGINT"
	case syscall.SIGTERM:
		name = "SIGTERM"
	}
	return fmt.Sprintf("musterflow: canceled: interrupted by %s before the upstream answered\n", name)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Parse persistent flags BEFORE building the registry so --data-dir
	// and --no-dashboard take effect (cobra parses flags during
	// ExecuteContext, which runs after the registry is constructed).
	// See BUG-004 / DF-003 / DF-013.
	flagDataDir = parseDataDirFlag(os.Args[1:])
	flagNoDashboard = parseNoDashboardFlag(os.Args[1:])

	// Load config — honor --data-dir for config file location.
	if flagDataDir != "" {
		cfg, err = config.LoadWithDataDir(flagDataDir)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg.DataDir = flagDataDir
	}

	// Detect if the dashboard is already running.
	// Honor --dashboard-addr if provided (parsed from raw args before cobra
	// runs). Otherwise use the configured port.
	dashAddr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	flagDashboardAddr = parseDashboardAddrFlag(os.Args[1:])
	if flagDashboardAddr != "" {
		dashAddr = flagDashboardAddr
	}

	// The dashboard may require a bearer token (dashboard_token /
	// MUSTERFLOW_DASHBOARD_TOKEN); every CLI call that routes through it has to
	// present it. Set before any dashboard request is made.
	cli.SetDashboardToken(cfg.DashboardToken)

	// --no-dashboard forces the local registry path, skipping dashboard
	// detection entirely.
	dashRunning := false
	if !flagNoDashboard {
		dashRunning = isMusterflowDashboard(dashAddr)
	}

	// PERF-001: translate the config timeout into the engine's per-attempt
	// knob. The engine reads MUSTER_HTTP_TIMEOUT from the environment at
	// request time; musterflow cannot pass the deadline through the generated
	// command path, so the environment is the seam. An operator's own
	// MUSTER_HTTP_TIMEOUT export keeps precedence by design.
	applyTimeoutEnv(cfg)
	cli.SetOperationTimeout(cfg.Timeout)

	// Load registry.
	//
	// DF-033: an explicit --data-dir names the registry the user wants touched.
	// Routing every command through the dashboard would write into the SERVER's
	// registry instead and still print success, so the flag wins and the CLI
	// stays local. The one exception is when the dashboard demonstrably serves
	// that very directory (lock file live there, holder answering on dashAddr):
	// then routing writes exactly where --data-dir points.
	registry := app.NewRegistry(cfg.DataDir)
	explicitDataDir := flagDataDir != ""
	routed := false
	if dashRunning && !explicitDataDir {
		// Skip LoadReadOnly: DuckDB read-only open still fails with a
		// "Conflicting lock is held" error when the dashboard process holds
		// the write lock.  The registry stays empty (store == nil) — this is
		// fine because all commands that need registry data (list, connect,
		// disconnect, catalog, refresh, mcp) route through the dashboard HTTP
		// API via dashboardBaseURL.  Flow commands (create, list, run) also
		// route through the dashboard HTTP API via dashboardBaseURL.
		cli.SetDashboardURL(dashboardURL(dashAddr))
		routed = true
	}
	if !routed {
		if err := registry.Load(); err != nil {
			lock, locked := app.AsStoreLock(err)
			if locked && explicitDataDir && dashRunning &&
				lockServesRequestedDir(lock, cfg.DataDir, dashAddr) {
				// The dashboard holds this very directory, so routing writes
				// into the directory the user named. Say so — never switch
				// registry paths silently.
				cli.SetDashboardURL(dashboardURL(dashAddr))
				fmt.Fprintf(os.Stderr,
					"note: --data-dir %s is served by the running dashboard at http://%s; routing this command through it\n",
					cfg.DataDir, dashAddr)
			} else {
				// DF-045: remember the failure so commands that need the
				// registry fail closed with an actionable error instead of
				// reporting an empty registry. A DuckDB lock conflict is
				// explained on demand (naming the lock holder and the fix), so
				// the raw driver text is not printed here.
				cli.SetStoreError(err)
				if !locked {
					fmt.Fprintf(os.Stderr, "Warning: could not load registry: %v\n", err)
				}
				if explicitDataDir && dashRunning {
					fmt.Fprint(os.Stderr, suppressedRoutingNote(cfg.DataDir, dashAddr))
				}
			}
		} else if explicitDataDir && dashRunning {
			fmt.Fprint(os.Stderr, suppressedRoutingNote(cfg.DataDir, dashAddr))
		}
	}

	// Set up auth manager for auto-injecting credentials into API commands
	authMgr := auth.NewManager(cfg)
	cli.SetAuthManager(authMgr)
	// Propagate --data-dir to CLI handlers for dir-aware config load/save
	cli.SetDataDir(flagDataDir)

	rootCmd := cli.NewRootCommand(registry)
	rootCmd.Version = Version

	// Add CLI flags
	rootCmd.PersistentFlags().StringVar(&flagDashboardAddr, "dashboard-addr", "", "Dashboard HTTP address (default: port from config)")
	rootCmd.PersistentFlags().StringVar(&flagDataDir, "data-dir", "", "Data directory (default: ~/.musterflow)")
	rootCmd.PersistentFlags().BoolVar(&flagNoDashboard, "no-dashboard", false, "Force local registry mode (skip dashboard detection)")

	// Override the start command to use config
	startCmd := findSubcommand(rootCmd, "start")
	if startCmd != nil {
		startCmd.RunE = func(cmd *cobra.Command, args []string) error {
			return startServer(registry, cfg)
		}
	}

	// Root command shows help by default
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// PERF-001: the dashboard's graceful-shutdown handler below (startServer)
	// owns the serving path. For every OTHER (one-shot) command a bare cancel
	// used to be a no-op — the engine's generated handlers request with an
	// inner context.Background(), so Ctrl-C silently did nothing and a
	// stalled call hung forever. Abort the process instead, with a message
	// naming the interruption.
	go func() {
		sig := <-sigCh
		if dashboardServing.Load() {
			cancel()
			return
		}
		fmt.Fprint(os.Stderr, signalAbortMessage(sig))
		os.Exit(1)
	}()

	// Auto-install shell completions on first run (unless disabled or non-interactive)
	if completion.ShouldPrompt(cfg.AutoCompletion) && isTerminal() {
		shell := completion.DetectShell()
		if completion.PromptInstall(shell) {
			installErr := completion.Install(shell, func(s completion.Shell) (string, error) {
				var buf bytes.Buffer
				switch s {
				case completion.ShellBash:
					return buf.String(), rootCmd.GenBashCompletion(&buf)
				case completion.ShellZsh:
					return buf.String(), rootCmd.GenZshCompletion(&buf)
				case completion.ShellFish:
					return buf.String(), rootCmd.GenFishCompletion(&buf, true)
				}
				return "", fmt.Errorf("unsupported shell: %s", s)
			})
			if installErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: completion install failed: %v\n", installErr)
			}
		}
	}

	// DF-045: cobra reports `unknown command "<api>"` when the registry is
	// unusable (the per-API subcommands were never registered), which hides the
	// real cause. Replace that with the store error.
	return cli.ResolveExecutionError(rootCmd.ExecuteContext(ctx))
}

func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}

func startServer(registry *app.Registry, cfg config.Config) error {
	// The signal goroutine in run() must not hard-exit a serving dashboard:
	// startServer owns the graceful shutdown there.
	dashboardServing.Store(true)

	// DF-045: refuse to start a second dashboard over an unreadable registry —
	// it would serve an empty API list while another process owns the store.
	if err := cli.StoreUnavailable(); err != nil {
		return err
	}

	// Resolve the listen address. The host from --dashboard-addr is preserved
	// (loopback when the flag is host-less or absent): the dashboard used to
	// discard it and listen on ":<port>", which binds every interface.
	requested, err := dashboard.ListenAddr(flagDashboardAddr, cfg.Port)
	if err != nil {
		return err
	}
	host, portStr, err := net.SplitHostPort(requested)
	if err != nil {
		return fmt.Errorf("invalid --dashboard-addr %q: want host:port or :port", flagDashboardAddr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("invalid --dashboard-addr %q: %q is not a port between 0 and 65535", flagDashboardAddr, portStr)
	}

	// Port selection (GAP-018): a foreign process on the configured port is
	// survived by walking the documented range [port, port+9], while an
	// existing MusterFlow dashboard is refused rather than bypassed — see
	// selectStartPort.
	choice, err := selectStartPort(port, dashboardPortOccupied)
	if err != nil {
		return err
	}
	if choice.Note != "" {
		fmt.Println(choice.Note)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(choice.Port))

	// Build tool registry from connected APIs
	toolRegistry := mcp.NewToolRegistry(registry)
	catalogClient := catalog.NewClient()

	// Construction (not ListenAndServe) enforces the fail-closed policy: a
	// non-loopback bind with no dashboard_token is refused here, before the
	// banner claims a dashboard is up.
	dashServer, err := dashboard.NewServerWithToken(registry, catalogClient, toolRegistry, addr, cfg.DashboardToken)
	if err != nil {
		return err
	}

	fmt.Printf("⚡ MusterFlow %s\n\n", Version)
	url := dashboard.BaseURL(addr)
	fmt.Printf("Dashboard:    %s\n", url)
	fmt.Printf("API:          %s/api/\n", url)
	fmt.Printf("MCP endpoint: %s/mcp\n", url)
	if dashServer.AuthEnabled() {
		fmt.Println("Auth:         bearer token required (dashboard_token)")
	}
	fmt.Printf("\nConnected APIs: %d\n", len(registry.List()))
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop.")

	// Build MCP handler registry
	handlerReg := handlers.NewRegistry()
	handlerReg.Register(handlers.NewInitializeHandler("musterflow-mcp", Version))
	handlerReg.Register(handlers.NewInitializedHandler())
	handlerReg.Register(handlers.NewListToolsHandler(toolRegistry))
	handlerReg.Register(handlers.NewCallToolHandler(toolRegistry))

	mcpHTTPServer := mcp.NewHTTPServer(handlerReg)
	dashServer.SetMCPHandler(mcpHTTPServer)

	// GAP-016: Retry the boot refresh with exponential backoff so a transient
	// failure (network down, spec host unreachable) recovers without restart.
	// Run asynchronously — do NOT block server startup. The retry loop stops
	// when refresh succeeds (tools appear) or the process shuts down.
	refreshCtx, refreshCancel := context.WithCancel(context.Background())
	go func() {
		_ = mcp.RefreshWithRetry(refreshCtx, toolRegistry.Refresh, mcp.DefaultRefreshBackoff, func(err error) {
			fmt.Fprintf(os.Stderr, "Warning: MCP tool refresh: %v\n", err)
		})
	}()

	httpServer := &http.Server{
		Addr:    addr,
		Handler: dashServer.Handler(),
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()

	fmt.Println("\nShutting down...")
	refreshCancel()
	_ = httpServer.Shutdown(ctx)
	wg.Wait()
	fmt.Println("Goodbye.")
	return nil
}

// isTerminal returns true if stdin is a terminal (interactive session).
// Uses golang.org/x/term which correctly distinguishes a real TTY from
// character devices like /dev/null that report ModeCharDevice but are not
// interactive — preventing the completion prompt from blocking in cron,
// pipes, and other non-interactive contexts.
func isTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// dashboardURL renders the base URL for a detected dashboard address.
func dashboardURL(addr string) string {
	return fmt.Sprintf("http://%s", addr)
}

// suppressedRoutingNote is the single stderr line DF-033 prints when an
// explicit --data-dir keeps the CLI local while a dashboard is running: the
// command is about to touch the named directory, NOT the dashboard's registry.
func suppressedRoutingNote(dataDir, dashAddr string) string {
	return fmt.Sprintf(
		"note: --data-dir %s given, so dashboard routing at %s is disabled; the running dashboard keeps its own registry\n",
		dataDir, dashboardURL(dashAddr))
}

// addrPort extracts the port from a "host:port" or ":port" address, or 0 when
// the address has no parseable port.
func addrPort(addr string) int {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return 0
	}
	var port int
	if _, err := fmt.Sscanf(addr[idx+1:], "%d", &port); err != nil {
		return 0
	}
	return port
}

// dashboardServesDataDir reports whether the DuckDB lock holder's database
// lives in dataDir — i.e. the dashboard already serves the exact directory the
// user named, so routing a command through it writes where --data-dir points
// rather than into some other environment's registry (DF-033).
func dashboardServesDataDir(lock *app.StoreLock, dataDir string) bool {
	if lock == nil || lock.DBPath == "" || dataDir == "" {
		return false
	}
	lockDir, err := filepath.Abs(filepath.Dir(lock.DBPath))
	if err != nil {
		return false
	}
	wantDir, err := filepath.Abs(dataDir)
	if err != nil {
		return false
	}
	return lockDir == wantDir
}

// lockServesRequestedDir reports whether the lock conflict proves the dashboard
// detected at dashAddr already serves dataDir. Both halves are required: the
// holder's database sits in dataDir, AND the holder answers MusterFlow's health
// probe on the very port we detected (otherwise a different musterflow process
// holds the lock and routing would again target the wrong registry).
func lockServesRequestedDir(lock *app.StoreLock, dataDir, dashAddr string) bool {
	if lock == nil {
		return false
	}
	port := addrPort(dashAddr)
	return port != 0 && lock.Port == port && dashboardServesDataDir(lock, dataDir)
}

// isMusterflowDashboard verifies that a musterflow dashboard is actually
// listening on the given address by performing an HTTP GET to /api/health
// and checking that the response is JSON with a "status" field equal to
// "ok". A bare TCP listener (e.g. a foreign process squatting on the port)
// will fail this check and be treated as "no dashboard running".
//
// This replaces the old isPortInUse check which treated ANY TCP listener as
// the dashboard — a foreign process on :9876 (such as a dagger engine) would
// cause musterflow connect/list to fail with JSON decode errors. See DF-013.
func isMusterflowDashboard(addr string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/health")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return false
	}
	return health.Status == "ok"
}

// parseDashboardAddrFlag extracts the --dashboard-addr value from raw CLI
// args. Cobra only parses persistent flags during ExecuteContext, which runs
// after the registry is constructed — so the flag must be read manually here
// for dashboard detection to honor it. Supports both
// "--dashboard-addr <value>" and "--dashboard-addr=<value>" forms.
func parseDashboardAddrFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--dashboard-addr" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(args[i], "--dashboard-addr=") {
			return strings.TrimPrefix(args[i], "--dashboard-addr=")
		}
	}
	return ""
}

// parseDataDirFlag extracts the --data-dir value from raw CLI args.
// Cobra only parses persistent flags during ExecuteContext, which runs after
// the registry is constructed — so the flag must be read manually here for
// the registry to honor it. Supports both "--data-dir <path>" and
// "--data-dir=<path>" forms.
func parseDataDirFlag(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--data-dir" && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(args[i], "--data-dir=") {
			return strings.TrimPrefix(args[i], "--data-dir=")
		}
	}
	return ""
}

// parseNoDashboardFlag detects the --no-dashboard flag from raw CLI args.
// Returns true if the flag is present (in either "--no-dashboard" or
// "--no-dashboard=true" form). Used to force local registry mode before
// cobra parses flags. See DF-013.
func parseNoDashboardFlag(args []string) bool {
	for _, a := range args {
		if a == "--no-dashboard" {
			return true
		}
		if strings.HasPrefix(a, "--no-dashboard=") {
			v := strings.TrimPrefix(a, "--no-dashboard=")
			return v == "true" || v == "1"
		}
	}
	return false
}
