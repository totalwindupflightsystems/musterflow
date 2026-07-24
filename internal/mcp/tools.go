package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	musterMcp "github.com/wojons/muster/pkg/mcp"
	"github.com/wojons/muster/pkg/mcp/handlers"
	"github.com/wojons/muster/pkg/openapi"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// ToolRegistry maps connected APIs to MCP tool descriptors.
// It reads from the app.Registry, fetches and parses each API's OpenAPI spec,
// and generates tool definitions with JSON schemas for input parameters.
type ToolRegistry struct {
	mu          sync.RWMutex
	appRegistry *app.Registry
	tools       []handlers.Tool
	toolConfigs map[string]musterMcp.ExecutionConfig
}

// NewToolRegistry creates a new ToolRegistry backed by the given app registry.
func NewToolRegistry(appReg *app.Registry) *ToolRegistry {
	return &ToolRegistry{
		appRegistry: appReg,
		tools:       make([]handlers.Tool, 0),
		toolConfigs: make(map[string]musterMcp.ExecutionConfig),
	}
}

// Refresh reloads tool definitions from all connected APIs.
// It fetches each API's OpenAPI spec, converts operations to MCP tools,
// and merges them into the registry. A single API failure is skipped
// silently — the method returns an error only if ALL APIs fail to load
// (and there is at least one API).
func (tr *ToolRegistry) Refresh() error {
	conns := tr.appRegistry.List()

	tr.mu.Lock()
	defer tr.mu.Unlock()

	// Reset state for a fresh merge
	tr.tools = tr.tools[:0]
	tr.toolConfigs = make(map[string]musterMcp.ExecutionConfig)

	if len(conns) == 0 {
		return nil
	}

	var allErrors []error
	successCount := 0

	for _, conn := range conns {
		// Fetch the OpenAPI spec
		data, err := fetchSpecData(conn.SpecURL)
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("api %q: fetch spec: %w", conn.Name, err))
			continue
		}

		// Parse the spec
		parser := openapi.NewParser()
		result, err := parser.Parse(context.Background(), data, openapi.DefaultParseOptions())
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("api %q: parse spec: %w", conn.Name, err))
			continue
		}

		// Use the connection's BaseURL, falling back to spec's first server
		baseURL := conn.BaseURL
		if baseURL == "" && result.Document != nil && len(result.Document.Servers) > 0 {
			baseURL = result.Document.Servers[0].URL
		}

		// Convert OpenAPI operations to MCP tools
		converter := &musterMcp.Converter{BaseURL: baseURL}
		openAPITools, configs, err := converter.ConvertToTools(result.Document)
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("api %q: convert to tools: %w", conn.Name, err))
			continue
		}

		// Merge tools and configs
		for _, tool := range openAPITools {
			hTool := tool.ToHandlersTool()
			tr.tools = append(tr.tools, hTool)
			tr.toolConfigs[hTool.Name] = configs[hTool.Name]
		}

		successCount++
	}

	// Only return error if ALL APIs failed
	if successCount == 0 && len(allErrors) > 0 {
		return fmt.Errorf("all APIs failed to load: %v", allErrors)
	}

	return nil
}

// ListTools returns all tools from all connected APIs.
// Implements handlers.ToolLister so NewListToolsHandler uses direct tool list
// (preserves InputSchema).
func (tr *ToolRegistry) ListTools() []handlers.Tool {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	tools := make([]handlers.Tool, len(tr.tools))
	copy(tools, tr.tools)
	return tools
}

// Execute executes a tool by name with the given arguments.
// It looks up the tool's execution config and uses musterMcp.ExecuteHTTP
// to perform the HTTP request to the target API.
func (tr *ToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	tr.mu.RLock()
	config, ok := tr.toolConfigs[name]
	tr.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}

	result, err := musterMcp.ExecuteHTTP(config.BaseURL, config.Method, config.Path, args)
	if err != nil {
		return nil, err
	}

	var v interface{}
	if err := json.Unmarshal(result, &v); err != nil {
		// Not JSON — return as string
		return string(result), nil
	}
	return v, nil
}

// fetchSpecData loads OpenAPI spec data from a URL or local file path.
// Mirrors the pattern in internal/app/connect.go.
func fetchSpecData(specURL string) ([]byte, error) {
	if strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://") {
		resp, err := http.Get(specURL)
		if err != nil {
			return nil, fmt.Errorf("http get: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("http status %d", resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
	return os.ReadFile(specURL)
}

// --- CommandRegistry interface stubs ---
// ToolRegistry implements handlers.CommandRegistry so it can be passed to
// handlers.NewListToolsHandler. The list handler first checks for the
// ToolLister interface (which we implement via ListTools), so these
// methods are only needed for compile-time interface satisfaction.

// ListCommands returns commands derived from the tool definitions.
func (tr *ToolRegistry) ListCommands() []handlers.Command {
	tools := tr.ListTools()
	commands := make([]handlers.Command, 0, len(tools))
	for _, tool := range tools {
		commands = append(commands, handlers.Command{
			Name:        tool.Name,
			Description: tool.Description,
		})
	}
	return commands
}

// GetCommand returns a command by name, or nil if not found.
func (tr *ToolRegistry) GetCommand(name string) *handlers.Command {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	for _, tool := range tr.tools {
		if tool.Name == name {
			return &handlers.Command{
				Name:        tool.Name,
				Description: tool.Description,
			}
		}
	}
	return nil
}

// ExecuteCommand executes a tool by name (implements CommandRegistry).
func (tr *ToolRegistry) ExecuteCommand(ctx context.Context, name string, params map[string]interface{}) (interface{}, error) {
	return tr.Execute(ctx, name, params)
}

// AddCommand is not supported for the tool registry.
func (tr *ToolRegistry) AddCommand(cmd handlers.Command) error {
	return fmt.Errorf("not supported in tool registry")
}

// RemoveCommand is not supported for the tool registry.
func (tr *ToolRegistry) RemoveCommand(name string) error {
	return fmt.Errorf("not supported in tool registry")
}

// UpdateCommand is not supported for the tool registry.
func (tr *ToolRegistry) UpdateCommand(name string, cmd handlers.Command) error {
	return fmt.Errorf("not supported in tool registry")
}
