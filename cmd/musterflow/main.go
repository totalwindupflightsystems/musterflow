// MusterFlow — turn any API into a CLI, an MCP tool, and a workflow.
package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

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

	// CLI flags override config
	if flagDataDir != "" {
		cfg.DataDir = flagDataDir
	}

	// Detect if the dashboard is already running on the configured port.
	// If it is, open the DB read-only to avoid conflicting lock errors.
	dashAddr := fmt.Sprintf("127.0.0.1:%d", cfg.Port)
	dashRunning := isPortInUse(dashAddr)

	// Load registry
	registry := app.NewRegistry(cfg.DataDir)
	if dashRunning {
		if err := registry.LoadReadOnly(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load registry (read-only): %v\n", err)
		}
		// Route write operations (connect/disconnect) through the dashboard API
		cli.SetDashboardURL(fmt.Sprintf("http://%s", dashAddr))
	} else {
		if err := registry.Load(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not load registry: %v\n", err)
		}
	}

	// Set up auth manager for auto-injecting credentials into API commands
	authMgr := auth.NewManager(cfg)
	cli.SetAuthManager(authMgr)

	rootCmd := cli.NewRootCommand(registry)
	rootCmd.Version = Version

	// Add CLI flags
	rootCmd.PersistentFlags().StringVar(&flagDashboardAddr, "dashboard-addr", "", "Dashboard HTTP address (default: port from config)")
	rootCmd.PersistentFlags().StringVar(&flagDataDir, "data-dir", "", "Data directory (default: ~/.musterflow)")

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
	// Resolve port: CLI flag > config file > default with auto-discovery
	port := cfg.Port
	if flagDashboardAddr != "" {
		fmt.Sscanf(flagDashboardAddr, ":%d", &port)
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
	if err := toolRegistry.Refresh(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: MCP tool refresh: %v\n", err)
	}

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
	httpServer.Shutdown(ctx)
	wg.Wait()
	fmt.Println("Goodbye.")
	return nil
}

// isTerminal returns true if stdin is a terminal (interactive session).
func isTerminal() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

// isPortInUse returns true if something is listening on the given TCP address.
func isPortInUse(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
