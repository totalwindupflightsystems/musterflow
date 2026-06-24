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
	"github.com/totalwindupflightsystems/musterflow/internal/auth"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
	"github.com/totalwindupflightsystems/musterflow/internal/completion"
	"github.com/totalwindupflightsystems/musterflow/internal/config"
)

// apiCommandState tracks lazy generation of cobra commands for a connected API.
// Commands are generated on first access via sync.Once so startup stays fast.
type apiCommandState struct {
	once sync.Once
	err  error
}

var apiCommands = make(map[string]*apiCommandState)
var apiCommandsMu sync.Mutex

// auth command flags
var (
	typeFlag    string
	keyFlag     string
	certFlag    string
	keyPathFlag string
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
	root.AddCommand(newConfigCommand(registry))
	root.AddCommand(newAuthCommand(registry))
	root.AddCommand(newCompletionCommand())

	root.PersistentFlags().StringVarP(&outputFlag, "output", "o", "", "Output file path (format auto-detected from extension)")

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
			client := catalog.NewClient()
			results, err := client.Search(args[0])
			if err != nil {
				fmt.Printf("Error searching catalog: %v\n", err)
				return nil
			}
			if len(results) == 0 {
				fmt.Println("No catalog entries found.")
				return nil
			}
			fmt.Printf("Catalog search results (%d):\n\n", len(results))
			fmt.Printf("  %-20s %-20s %-6s %-60s %s\n", "ID", "NAME", "TYPE", "DESCRIPTION", "DOWNLOADS")
			for _, e := range results {
				desc := e.Description
				if len(desc) > 60 {
					desc = desc[:60]
				}
				fmt.Printf("  %-20s %-20s %-6s %-60s %d\n", e.ID, e.Name, e.Type, desc, e.Downloads)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "push <api-id>",
		Short: "Push a connected API to the community catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := registry.Get(args[0])
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
			fmt.Printf("Submit a PR to https://github.com/totalwindupflightsystems/musterflow-catalog with the entry JSON at entries/%s.json and add it to index.json\n", conn.ID)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "pull <api-id>",
		Short: "Pull an API from the community catalog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := catalog.NewClient()
			entry, _, err := client.FetchEntry(args[0])
			if err != nil {
				fmt.Printf("Error pulling from catalog: %v\n", err)
				return nil
			}
			if entry == nil {
				fmt.Printf("Entry %s not found in catalog.\n", args[0])
				return nil
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

func newConfigCommand(registry *app.Registry) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage MusterFlow configuration",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			// Mask auth keys
			for id, auth := range cfg.Auth {
				auth.Key = config.MaskKey(auth.Key)
				cfg.Auth[id] = auth
			}
			fmt.Printf("Port:              %d\n", cfg.Port)
			fmt.Printf("Data directory:    %s\n", cfg.DataDir)
			fmt.Printf("Default format:    %s\n", cfg.DefaultFormat)
			fmt.Printf("Auto-completion:   %v\n", cfg.AutoCompletion)
			fmt.Printf("Config file:       %s\n", config.ConfigPath())
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
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			key, val := args[0], args[1]
			switch key {
			case "port":
				fmt.Sscanf(val, "%d", &cfg.Port)
			case "default_format":
				cfg.DefaultFormat = val
			case "auto_completion":
				cfg.AutoCompletion = val == "true" || val == "1" || val == "yes"
			case "data_dir":
				cfg.DataDir = val
			default:
				return fmt.Errorf("unknown config key: %s (valid: port, default_format, auto_completion, data_dir)", key)
			}
			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
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
			cfg, err := config.Load()
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
			cfg, err := config.Load()
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
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			mgr := auth.NewManager(cfg)
			if err := mgr.Remove(args[0]); err != nil {
				return err
			}
			fmt.Printf("✓ Removed auth for %s\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <api-id>",
		Short: "Get the credential value for an API (prints raw key)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
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
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			mgr := auth.NewManager(cfg)

			// OAuth2 skeleton: for MVP, prompt the user to provide the token
			// manually. Full OAuth2 flow (browser open, callback server) is
			// a Phase 2 item.
			conn, err := registry.Get(args[0])
			if err != nil {
				return fmt.Errorf("API %q not found in registry. Connect it first with 'musterflow connect'", args[0])
			}
			fmt.Printf("Starting OAuth2 flow for %s (%s)\n", conn.Name, conn.SpecURL)
			fmt.Println()
			fmt.Println("OAuth2 browser-based flow is not yet implemented.")
			fmt.Println("For now, obtain a token manually and run:")
			fmt.Printf("  musterflow auth add %s --type oauth2 --key <token>\n", args[0])
			fmt.Println()
			fmt.Println("The full OAuth2 authorization code flow (browser open, callback")
			fmt.Println("server, token refresh) is planned for Phase 2.")
			_ = mgr // reserved for OAuth2 flow implementation
			return nil
		},
	})

	// Flags for the 'add' subcommand — scoped to the auth command
	cmd.PersistentFlags().StringVar(&typeFlag, "type", "bearer", "Auth type: none, apikey, bearer, oauth2, mtls")
	cmd.PersistentFlags().StringVar(&keyFlag, "key", "", "API key or bearer token")
	cmd.PersistentFlags().StringVar(&certFlag, "cert", "", "mTLS client certificate path")
	cmd.PersistentFlags().StringVar(&keyPathFlag, "key-path", "", "mTLS client key path")

	return cmd
}

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
				return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
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
