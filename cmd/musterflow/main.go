// MusterFlow — turn any API into a CLI, an MCP tool, and a workflow.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/cli"
	"github.com/totalwindupflightsystems/musterflow/internal/dashboard"
)

var (
	Version   = "0.1.0"
	Commit    = "unknown"
	BuildDate = "unknown"

	// Flags
	dashboardAddr string
	mcpAddr       string
	dataDir       string
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load registry
	regDir := dataDir
	if regDir == "" {
		regDir = app.DefaultDataDir()
	}
	registry := app.NewRegistry(regDir)
	if err := registry.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load registry: %v\n", err)
	}

	rootCmd := cli.NewRootCommand(registry)
	rootCmd.Version = Version

	// Add start flags
	rootCmd.PersistentFlags().StringVar(&dashboardAddr, "dashboard-addr", "localhost:9876", "Dashboard HTTP address")
	rootCmd.PersistentFlags().StringVar(&mcpAddr, "mcp-addr", "localhost:9876", "MCP endpoint address")
	rootCmd.PersistentFlags().StringVar(&dataDir, "data-dir", "", "Data directory (default: ~/.musterflow)")

	// Override the start command to actually start
	startCmd := findSubcommand(rootCmd, "start")
	if startCmd != nil {
		startCmd.RunE = func(cmd *cobra.Command, args []string) error {
			return startServer(registry)
		}
	}

	// Root command just shows help by default
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

func startServer(registry *app.Registry) error {
	addr := dashboardAddr
	fmt.Printf("⚡ MusterFlow %s\n\n", Version)
	fmt.Printf("Dashboard:    http://%s\n", addr)
	fmt.Printf("API:          http://%s/api/\n", addr)
	fmt.Printf("MCP endpoint: http://%s/mcp\n", addr)
	fmt.Printf("\nConnected APIs: %d\n", len(registry.List()))
	fmt.Println()
	fmt.Println("Press Ctrl+C to stop.")

	dashServer := dashboard.NewServer(registry, addr)

	// Add MCP endpoint handler
	mux := http.NewServeMux()
	mux.Handle("/", dashServer.Handler())
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32000,"message":"MCP endpoint available — connect via MCP client stdio or SSE"}}`)
	})

	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		}
	}()

	// Wait for interrupt
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
