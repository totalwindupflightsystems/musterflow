// MusterFlow — turn any API into a CLI, an MCP tool, and a workflow.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
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

	// CLI flag overrides
	flagDashboardAddr string
	flagDataDir       string
	flagNoDashboard   bool
)

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

	// --no-dashboard forces the local registry path, skipping dashboard
	// detection entirely.
	dashRunning := false
	if !flagNoDashboard {
		dashRunning = isMusterflowDashboard(dashAddr)
	}

	// Load registry
	registry := app.NewRegistry(cfg.DataDir)
	if dashRunning {
		// Skip LoadReadOnly: DuckDB read-only open still fails with a
		// "Conflicting lock is held" error when the dashboard process holds
		// the write lock.  The registry stays empty (store == nil) — this is
		// fine because all commands that need registry data (list, connect,
		// disconnect, catalog, refresh, mcp) route through the dashboard HTTP
		// API via dashboardBaseURL.  Flow commands (create, list, run) also
		// route through the dashboard HTTP API via dashboardBaseURL.
		cli.SetDashboardURL(fmt.Sprintf("http://%s", dashAddr))
	} else {
		if err := registry.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load registry: %v\n", err)
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

	go func() {
		<-sigCh
		cancel()
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

	return rootCmd.ExecuteContext(ctx)
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
	// Resolve port: CLI flag > config file > default with auto-discovery.
	// The --dashboard-addr flag may be in "host:port" or ":port" form.
	port := cfg.Port
	if flagDashboardAddr != "" {
		// Extract the port from either "host:port" or ":port" form.
		// fmt.Sscanf does not support %*[^:] suppression, so use
		// strings.LastIndex to find the final colon.
		if idx := strings.LastIndex(flagDashboardAddr, ":"); idx >= 0 {
			_, _ = fmt.Sscanf(flagDashboardAddr[idx+1:], "%d", &port)
		}
	}

	port, err := config.FindPort(port)
	if err != nil {
		return fmt.Errorf("no available port: %w", err)
	}

	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("⚡ MusterFlow %s\n\n", Version)
	fmt.Printf("Dashboard:    http://localhost%s\n", addr)
	fmt.Printf("API:          http://localhost%s/api/\n", addr)
	fmt.Printf("MCP endpoint: http://localhost%s/mcp\n", addr)
	fmt.Printf("\nConnected APIs: %d\n", len(registry.List()))
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop.")

	// Build tool registry from connected APIs
	toolRegistry := mcp.NewToolRegistry(registry)

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

	catalogClient := catalog.NewClient()
	dashServer := dashboard.NewServer(registry, catalogClient, toolRegistry, addr)

	// Build MCP handler registry
	handlerReg := handlers.NewRegistry()
	handlerReg.Register(handlers.NewInitializeHandler("musterflow-mcp", Version))
	handlerReg.Register(handlers.NewInitializedHandler())
	handlerReg.Register(handlers.NewListToolsHandler(toolRegistry))
	handlerReg.Register(handlers.NewCallToolHandler(toolRegistry))

	mcpHTTPServer := mcp.NewHTTPServer(handlerReg)
	dashServer.SetMCPHandler(mcpHTTPServer)

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
