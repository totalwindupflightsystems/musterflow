// Package cli provides the cobra command tree for the musterflow CLI.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/openapi"
	"github.com/getkin/kin-openapi/openapi3"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/auth"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
	"github.com/totalwindupflightsystems/musterflow/internal/completion"
	"github.com/totalwindupflightsystems/musterflow/internal/config"
	"github.com/totalwindupflightsystems/musterflow/internal/wasm"
	"github.com/totalwindupflightsystems/musterflow/internal/workflow"
)

// apiCommandState tracks lazy generation of cobra commands for a connected API.
// Commands are generated on first access via sync.Once so startup stays fast.
type apiCommandState struct {
	once sync.Once
	err  error
}

var apiCommands = make(map[string]*apiCommandState)
var apiCommandsMu sync.Mutex

// authMgr is set by the main function and used by BuildRequest for auto-auth.
var authMgr *auth.Manager

// SetAuthManager sets the global auth manager for auto-injecting credentials.
func SetAuthManager(m *auth.Manager) {
	authMgr = m
}

// dashboardBaseURL is set by main when the dashboard is detected as running.
// When non-empty, CLI write operations (connect/disconnect) route through the
// dashboard HTTP API instead of opening the DB directly.
var dashboardBaseURL string

// SetDashboardURL sets the dashboard base URL so CLI write operations route
// through the dashboard API (avoiding DuckDB lock conflicts).
func SetDashboardURL(url string) {
	dashboardBaseURL = url
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
	root.AddCommand(newExportCommand(registry))
	root.AddCommand(newImportCommand(registry))
	root.AddCommand(newRefreshCommand(registry))
	root.AddCommand(newTransformCommand())

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

			// If the dashboard is running, route through its API to avoid lock conflicts.
			if dashboardBaseURL != "" {
				return connectViaDashboard(specURL, baseURL, nameInput, authType)
			}

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
			if dashboardBaseURL != "" {
				return listViaDashboard()
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
			if err := app.Disconnect(registry, args[0]); err != nil {
				return err
			}
			fmt.Printf("✓ Disconnected: %s\n", args[0])
			return nil
		},
	}
}

// connectViaDashboard routes a connect operation through the dashboard HTTP API
// to avoid DuckDB lock conflicts when the dashboard holds the write lock.
func connectViaDashboard(specURL, baseURL, nameInput, authType string) error {
	payload := map[string]interface{}{
		"spec_url":  specURL,
		"base_url":  baseURL,
		"name":      nameInput,
		"auth_type": authType,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal connect payload: %w", err)
	}

	resp, err := http.Post(dashboardBaseURL+"/api/apis", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dashboard connect request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		SpecTitle     string `json:"spec_title"`
		SpecVersion   string `json:"spec_version"`
		EndpointCount int    `json:"endpoint_count"`
		BaseURL       string `json:"base_url"`
		Error         string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("connect via dashboard: %s", msg)
	}

	fmt.Printf("✓ Connected: %s\n", result.SpecTitle)
	fmt.Printf("  ID: %s\n", result.ID)
	fmt.Printf("  Version: %s\n", result.SpecVersion)
	fmt.Printf("  Endpoints: %d\n", result.EndpointCount)
	fmt.Printf("  Base URL: %s\n", result.BaseURL)
	fmt.Printf("\nTry: musterflow %s --help\n", result.Name)
	return nil
}

// disconnectViaDashboard routes a disconnect operation through the dashboard HTTP API
// to avoid DuckDB lock conflicts when the dashboard holds the write lock.
func disconnectViaDashboard(apiID string) error {
	req, err := http.NewRequest(http.MethodDelete, dashboardBaseURL+"/api/apis/"+apiID, nil)
	if err != nil {
		return fmt.Errorf("create disconnect request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dashboard disconnect request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("disconnect via dashboard: %s", msg)
	}

	fmt.Printf("✓ Disconnected: %s\n", apiID)
	return nil
}

// listViaDashboard fetches the connected API list from the dashboard HTTP API
// to avoid DuckDB lock conflicts when the dashboard holds the write lock.
func listViaDashboard() error {
	resp, err := http.Get(dashboardBaseURL + "/api/apis")
	if err != nil {
		return fmt.Errorf("dashboard list request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		APIs []*app.APIConnection `json:"apis"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	if len(result.APIs) == 0 {
		fmt.Println("No APIs connected.")
		fmt.Println("Run 'musterflow connect <url>' to add one.")
		return nil
	}

	fmt.Printf("Connected APIs (%d):\n\n", len(result.APIs))
	for _, conn := range result.APIs {
		fmt.Printf("  %s (%s)\n", conn.Name, conn.ID)
		fmt.Printf("    Spec: %s\n", conn.SpecURL)
		fmt.Printf("    Base: %s\n", conn.BaseURL)
		fmt.Printf("    Endpoints: %d\n", conn.EndpointCount)
		fmt.Printf("    Auth: %s\n", conn.AuthType)
		fmt.Println()
	}
	return nil
}

// catalogSearchViaDashboard searches the catalog via the dashboard HTTP API.
func catalogSearchViaDashboard(query string) error {
	resp, err := http.Get(dashboardBaseURL + "/api/catalog/search?q=" + query)
	if err != nil {
		return fmt.Errorf("dashboard catalog search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("dashboard returned HTTP %d", resp.StatusCode)
	}

	var result struct {
		Results []catalog.CatalogEntry `json:"results"`
		Total   int                    `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	if len(result.Results) == 0 {
		fmt.Println("No catalog entries found.")
		return nil
	}
	fmt.Printf("Catalog search results (%d):\n\n", len(result.Results))
	fmt.Printf("  %-20s %-20s %-6s %-60s %s\n", "ID", "NAME", "TYPE", "DESCRIPTION", "DOWNLOADS")
	for _, e := range result.Results {
		desc := e.Description
		if len(desc) > 60 {
			desc = desc[:60]
		}
		fmt.Printf("  %-20s %-20s %-6s %-60s %d\n", e.ID, e.Name, e.Type, desc, e.Downloads)
	}
	return nil
}

// refreshViaDashboard routes a refresh operation through the dashboard HTTP API.
func refreshViaDashboard(apiID string) error {
	resp, err := http.Post(dashboardBaseURL+"/api/apis/"+apiID+"/refresh", "application/json", nil)
	if err != nil {
		return fmt.Errorf("dashboard refresh request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Name           string `json:"name"`
		OldVersion     string `json:"old_version"`
		NewVersion     string `json:"new_version"`
		VersionChanged bool   `json:"version_changed"`
		OldEndpoints   int    `json:"old_endpoints"`
		NewEndpoints   int    `json:"new_endpoints"`
		Error          string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("refresh via dashboard: %s", msg)
	}

	fmt.Printf("✓ Refreshed %s\n", result.Name)
	fmt.Printf("  Version: %s → %s", result.OldVersion, result.NewVersion)
	if result.VersionChanged {
		fmt.Print(" (changed)")
	}
	fmt.Println()
	fmt.Printf("  Endpoints: %d → %d\n", result.OldEndpoints, result.NewEndpoints)
	return nil
}

// pushViaDashboard fetches an API connection from the dashboard and prints the catalog entry JSON.
func pushViaDashboard(apiID string) error {
	resp, err := http.Get(dashboardBaseURL + "/api/apis/" + apiID)
	if err != nil {
		return fmt.Errorf("dashboard get request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct{ Error string `json:"error"` }
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		msg := errBody.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("get api via dashboard: %s", msg)
	}

	var conn app.APIConnection
	if err := json.NewDecoder(resp.Body).Decode(&conn); err != nil {
		return fmt.Errorf("decode connection: %w", err)
	}

	entry := catalog.ConnectionToCatalogEntry(&conn)
	data, err := entry.ToJSON()
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	fmt.Printf("Catalog entry JSON for %s:\n\n", conn.ID)
	fmt.Println(string(data))
	fmt.Println()
	fmt.Printf("Submit a PR to https://github.com/totalwindupflightsystems/musterflow-catalog with the entry JSON at entries/%s.json and add it to index.json\n", conn.ID)
	return nil
}

// pullViaDashboard pulls an API from the catalog via the dashboard HTTP API.
func pullViaDashboard(apiID string) error {
	client := catalog.NewClient()
	entry, _, err := client.FetchEntry(apiID)
	if err != nil {
		fmt.Printf("Error pulling from catalog: %v\n", err)
		return nil
	}
	if entry == nil {
		fmt.Printf("Entry %s not found in catalog.\n", apiID)
		return nil
	}
	fmt.Printf("Pulling %s (%s) from community catalog...\n", entry.ID, entry.Name)

	// Route through dashboard connect endpoint
	payload := map[string]interface{}{
		"spec_url": entry.SpecURL,
		"name":     entry.Name,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal pull payload: %w", err)
	}

	resp, err := http.Post(dashboardBaseURL+"/api/apis", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("dashboard pull request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		SpecTitle     string `json:"spec_title"`
		EndpointCount int    `json:"endpoint_count"`
		Error         string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode dashboard response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("dashboard returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("pull via dashboard: %s", msg)
	}

	fmt.Printf("✓ Pulled and connected: %s\n", result.SpecTitle)
	fmt.Printf("  ID: %s\n", result.ID)
	fmt.Printf("  Endpoints: %d\n", result.EndpointCount)
	return nil
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
			if dashboardBaseURL != "" {
				return catalogSearchViaDashboard(args[0])
			}
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
			if dashboardBaseURL != "" {
				return pushViaDashboard(args[0])
			}
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
			if dashboardBaseURL != "" {
				return pullViaDashboard(args[0])
			}
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

	var (
		webhook    bool
		description string
	)

	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := workflow.NewEngine(
				filepath.Join(app.DefaultDataDir(), "flows"),
				"http://localhost:9876",
			)
			flow, err := engine.Create(args[0], "# Write your Starlark workflow here\n", webhook)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Created flow %q\n", flow.Name)
			if webhook {
				fmt.Printf("  Webhook URL: %s\n", flow.WebhookURL)
			}
			fmt.Printf("  Edit: %s/flows/%s.star\n", app.DefaultDataDir(), flow.Name)
			return nil
		},
	}
	createCmd.Flags().BoolVar(&webhook, "webhook", false, "Create a webhook trigger for this flow")
	createCmd.Flags().StringVar(&description, "description", "", "Flow description")
	cmd.AddCommand(createCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List workflows",
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := workflow.NewEngine(
				filepath.Join(app.DefaultDataDir(), "flows"),
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
				if f.WebhookURL != "" {
					fmt.Printf("  webhook: %s", f.WebhookURL)
				}
				fmt.Println()
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "run <name>",
		Short: "Run a workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := workflow.NewEngine(
				filepath.Join(app.DefaultDataDir(), "flows"),
				"http://localhost:9876",
			)
			output, err := engine.Run(args[0], nil)
			if err != nil {
				return err
			}
			fmt.Print(output)
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
				_, _ = fmt.Sscanf(val, "%d", &cfg.Port)
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
			cfg2, err := config.Load()
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
			store := registry.Store()
			if store == nil {
				return fmt.Errorf("registry not loaded")
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
	return &cobra.Command{
		Use:   "import <path>",
		Short: "Import APIs from a JSONL file",
		Long:  "Import API connections from a JSONL file into the registry.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := registry.Store()
			if store == nil {
				return fmt.Errorf("registry not loaded")
			}
			n, err := app.ImportJSONL(store, args[0])
			if err != nil {
				return err
			}
			fmt.Printf("✓ Imported %d APIs from %s\n", n, args[0])
			return nil
		},
	}
}

func newRefreshCommand(registry *app.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh <api-id>",
		Short: "Refresh API spec and regenerate commands",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dashboardBaseURL != "" {
				return refreshViaDashboard(args[0])
			}
			result, err := app.Refresh(cmd.Context(), registry, args[0])
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
			return nil
		},
	}
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
			if err := reg.InstallFromCatalog(args[0]); err != nil {
				fmt.Println(err)
			}
			return nil
		},
	})

	return cmd
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
	// ValidArgsFunction provides dynamic completion for lazily-generated
	// API subcommands. Cobra's V2 bash completion (and built-in fish/zsh
	// completion) calls the binary with __complete at completion time,
	// which triggers this function to load and enumerate subcommands.
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if err := ensureAPILoaded(cmd, conn); err != nil {
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

// startCallbackServer starts an HTTP server on the given port and waits for
// a single OAuth2 callback. Returns the authorization code from the query.
func startCallbackServer(port int) (string, error) {
	type result struct {
		code string
		err  error
	}
	ch := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		code := q.Get("code")
		errStr := q.Get("error")
		if errStr != "" {
			ch <- result{err: fmt.Errorf("authorization error: %s (%s)", errStr, q.Get("error_description"))}
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}
		if code == "" {
			ch <- result{err: fmt.Errorf("no authorization code received")}
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			return
		}
		ch <- result{code: code}
		fmt.Fprint(w, "Authorization successful! You may close this window.")
	})

	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		// Server will exit on Shutdown or error
		_ = srv.ListenAndServe()
	}()

	// Wait for callback or timeout
	select {
	case res := <-ch:
		srv.Close()
		if res.err != nil {
			return "", res.err
		}
		return res.code, nil
	case <-time.After(5 * time.Minute):
		srv.Close()
		return "", fmt.Errorf("timed out waiting for authorization (5 minutes)")
	}
}
