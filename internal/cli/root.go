// Package cli provides the cobra command tree for the musterflow CLI.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/openapi"
	"github.com/getkin/kin-openapi/openapi3"

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
	cmd := &cobra.Command{
		Use:                conn.Name,
		Short:              fmt.Sprintf("Commands for %s API", conn.Name),
		Long:               conn.Description,
		DisableFlagParsing: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return ensureAPILoaded(cmd, conn)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ensureAPILoaded(cmd, conn); err != nil {
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
				return cmd.Help()
			}
			// Re-execute through cobra's pipeline starting at the target.
			// target is NOT our API command (which has DisableFlagParsing),
			// so this won't recurse — target handles its own flag parsing.
			target.SetArgs(remaining)
			return target.Execute()
		},
	}
	// Override help so lazy-loaded subcommands appear in --help output.
	// cobra resolves the command tree before PersistentPreRunE fires, so we
	// trigger the lazy load from the help func too.
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		_ = ensureAPILoaded(c, conn)
		// If args reference a subcommand, find and show its help.
		searchArgs := args
		for len(searchArgs) > 0 && searchArgs[0] == c.Name() {
			searchArgs = searchArgs[1:]
		}
		if len(searchArgs) > 0 {
			target, _, e := c.Find(searchArgs)
			if e == nil && target != nil && target != c {
				fmt.Fprint(target.OutOrStdout(), target.UsageString())
				return
			}
		}
		fmt.Fprint(c.OutOrStdout(), c.UsageString())
	})
	return cmd
}

// ensureAPILoaded lazily generates cobra commands for a connected API on first
// access via sync.Once. Safe to call from multiple paths (PersistentPreRunE,
// help func). Subsequent calls are no-ops.
func ensureAPILoaded(cmd *cobra.Command, conn *app.APIConnection) error {
	connID := conn.ID
	apiCommandsMu.Lock()
	state, exists := apiCommands[connID]
	if !exists {
		state = &apiCommandState{}
		apiCommands[connID] = state
	}
	apiCommandsMu.Unlock()

	state.once.Do(func() {
		state.err = loadAPICommands(cmd, conn)
	})
	return state.err
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

	// Clear per-operation server URLs so the generator falls back to
	// g.config.BaseURL (which is the fully-resolved base URL from the
	// connection, including scheme and host). Many specs have relative
	// server URLs like "/api/v3" that can't be used directly.
	doc := result.Document
	if doc != nil {
		doc.Servers = nil
		if doc.Paths != nil {
			for _, pathItem := range doc.Paths.Map() {
				if pathItem != nil {
					pathItem.Servers = nil
					clearOperationServers(pathItem.Get)
					clearOperationServers(pathItem.Post)
					clearOperationServers(pathItem.Put)
					clearOperationServers(pathItem.Patch)
					clearOperationServers(pathItem.Delete)
					clearOperationServers(pathItem.Head)
					clearOperationServers(pathItem.Options)
				}
			}
		}
	}

	genCfg := app.GenerateCommandConfig(conn)
	gen, err := generator.NewGenerator(*genCfg)
	if err != nil {
		return fmt.Errorf("create generator: %w", err)
	}

	commands, err := gen.GenerateCommands(doc)
	if err != nil {
		return fmt.Errorf("generate commands: %w", err)
	}

	for _, cmd := range commands {
		parent.AddCommand(cmd)
	}

	fmt.Fprintf(os.Stderr, "  ✓ Generated %d commands for %s\n", len(commands), conn.Name)
	return nil
}

// clearOperationServers clears the per-operation Servers field so the
// generator falls back to the config-level BaseURL.
func clearOperationServers(op *openapi3.Operation) {
	if op != nil {
		op.Servers = nil
	}
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
