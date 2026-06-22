// Package cli provides the cobra command tree for the musterflow CLI.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/openapi"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// NewRootCommand creates the root musterflow command.
func NewRootCommand(registry *app.Registry) *cobra.Command {
	root := &cobra.Command{
		Use:   "musterflow",
		Short: "Turn any API into a CLI, an MCP tool, and a workflow",
		Long: `MusterFlow turns any API into a typed CLI, an MCP endpoint, and a workflow engine.

Connect:    musterflow connect https://api.github.com
List:       musterflow list
Use:        musterflow gh issues list --state open
Workflow:   musterflow flow create`,
	}

	root.AddCommand(newStartCommand(registry))
	root.AddCommand(newConnectCommand(registry))
	root.AddCommand(newListCommand(registry))
	root.AddCommand(newDisconnectCommand(registry))
	root.AddCommand(newCatalogCommand(registry))
	root.AddCommand(newFlowCommand(registry))
	root.AddCommand(newMCPCommand(registry))

	loadAPISubcommands(root, registry)

	return root
}

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

	cmd := &cobra.Command{
		Use:   "connect <spec-url>",
		Short: "Connect an API from an OpenAPI spec URL or file path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specURL := args[0]
			result, err := app.Connect(cmd.Context(), registry, app.ConnectOptions{
				SpecURL:  specURL,
				BaseURL:  baseURL,
				Name:     nameInput,
				AuthType: authType,
			})
			if err != nil {
				return err
			}

			fmt.Printf("✓ Connected: %s\n", result.SpecTitle)
			fmt.Printf("  ID: %s\n", result.Connection.ID)
			fmt.Printf("  Version: %s\n", result.SpecVersion)
			fmt.Printf("  Endpoints: %d\n", result.EndpointCount)
			fmt.Printf("  Base URL: %s\n", result.Connection.BaseURL)
			fmt.Printf("\nTry: musterflow %s --help\n", result.Connection.Name)

			return nil
		},
	}

	cmd.Flags().StringVarP(&baseURL, "base-url", "u", "", "Override base URL for the API")
	cmd.Flags().StringVarP(&nameInput, "name", "n", "", "Human-readable name for the API")
	cmd.Flags().StringVar(&authType, "auth", "none", "Auth type: none, apikey, bearer, oauth2, mtls")

	return cmd
}

func newListCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List connected APIs",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if err := app.Disconnect(registry, args[0]); err != nil {
				return err
			}
			fmt.Printf("✓ Disconnected: %s\n", args[0])
			return nil
		},
	}
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
			fmt.Println("catalog search not yet implemented")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "push <api-id>",
		Short: "Push a connected API to the community catalog",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("catalog push not yet implemented")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "pull <api-id>",
		Short: "Pull an API from the community catalog",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("catalog pull not yet implemented")
			return nil
		},
	})

	return cmd
}

func newFlowCommand(registry *app.Registry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "flow",
		Short: "Workflow management",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "Create a new workflow",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("workflow creation not yet implemented")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("No workflows defined.")
			return nil
		},
	})

	return cmd
}

func newMCPCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "MCP endpoint information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("MCP endpoint: http://localhost:9876/mcp")
			fmt.Println("Transport: stdio JSON-RPC / HTTP SSE")
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

func loadAPISubcommands(root *cobra.Command, registry *app.Registry) {
	for _, conn := range registry.List() {
		apiCmd := createAPISubcommand(conn)
		root.AddCommand(apiCmd)
	}
}

func createAPISubcommand(conn *app.APIConnection) *cobra.Command {
	return &cobra.Command{
		Use:   conn.Name,
		Short: fmt.Sprintf("Commands for %s API", conn.Name),
		Long:  conn.Description,
	}
}

func loadAPICommands(parent *cobra.Command, conn *app.APIConnection) error {
	data, err := loadSpecData(conn.SpecURL)
	if err != nil {
		return fmt.Errorf("load spec: %w", err)
	}

	ctx := context.Background()
	parser := openapi.NewParser()
	result, err := parser.Parse(ctx, data, openapi.DefaultParseOptions())
	if err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	genCfg := app.GenerateCommandConfig(conn)
	gen, err := generator.NewGenerator(*genCfg)
	if err != nil {
		return fmt.Errorf("create generator: %w", err)
	}

	commands, err := gen.GenerateCommands(result.Document)
	if err != nil {
		return fmt.Errorf("generate commands: %w", err)
	}

	for _, cmd := range commands {
		parent.AddCommand(cmd)
	}

	fmt.Fprintf(os.Stderr, "  ✓ Generated %d commands for %s\n", len(commands), conn.Name)
	return nil
}

func loadSpecData(specURL string) ([]byte, error) {
	if strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://") {
		resp, err := http.Get(specURL)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(specURL)
}
