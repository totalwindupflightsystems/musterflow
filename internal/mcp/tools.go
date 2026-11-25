package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	musterMcp "github.com/wojons/muster/pkg/mcp"
	"github.com/wojons/muster/pkg/mcp/handlers"
	"github.com/wojons/muster/pkg/openapi"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// Tool names are namespaced per connected API (DF-039). The CLI already
// namespaces by API (`musterflow <api> widgets list-widgets`); the MCP surface
// did not, so two APIs that both define `listWidgets` fought over one registry
// slot keyed by operationId alone — the last API connected won, silently, and
// tools/call hit the wrong base URL. Every operation is therefore published as
// "<api-slug><separator><operationId>", and the bare operationId is published
// only while it is globally unique across the connected APIs.
const (
	// toolNameSeparator joins the API slug to the operation name. "__" is
	// inside the conventional MCP tool-name character set
	// ([A-Za-z0-9_-]) so no escaping is needed, and a double underscore is not
	// a word separator, so an operationId such as "list_widgets" stays
	// readable next to it ("echo-api__list_widgets").
	toolNameSeparator = "__"

	// maxAPISlugLen bounds the slug half of a namespaced tool name, leaving
	// most of muster's 128-character Tool.name budget to the operation name.
	maxAPISlugLen = 32

	// maxToolNameLen mirrors muster's conventional Tool.name bound
	// (pkg/mcp.MCPToolNamePattern: ^[a-zA-Z0-9_-]{1,128}$).
	maxToolNameLen = 128

	// fallbackAPISlug is used when a connection's name and id sanitize to
	// nothing, so a tool name is never published with an empty namespace.
	fallbackAPISlug = "api"
)

// toolNameDisallowed matches every character the conventional MCP tool-name
// pattern rejects, so a connection name can be squeezed into a slug.
var toolNameDisallowed = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

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

// convertedAPI is one connected API whose spec fetched, parsed and converted
// cleanly, held until every API's operations are in hand. Naming needs the
// whole set: whether the bare operationId is published depends on whether any
// other API also defines it (DF-039).
type convertedAPI struct {
	conn    *app.APIConnection
	slug    string
	tools   []musterMcp.OpenAPITool
	configs map[string]musterMcp.ExecutionConfig
}

// Refresh reloads tool definitions from all connected APIs.
// It fetches each API's OpenAPI spec, converts operations to MCP tools,
// and rebuilds the registry from the whole connection set — never by merging
// into the previous state, so a disconnected API's tools and execution configs
// cannot outlive it. A single API failure is skipped silently — the method
// returns an error only if ALL APIs fail to load (and there is at least one
// API).
//
// Tool naming (DF-039): each operation is published as
// "<slug>__<operationId>", where slug is derived from the connection's name.
// The bare operationId is additionally published only when exactly one
// connected API defines it; on a collision it is dropped, so tools/list can
// never serve the same name twice and tools/call can never reach a base URL
// chosen by "last connect wins".
func (tr *ToolRegistry) Refresh() error {
	conns := tr.appRegistry.List()

	tr.mu.Lock()
	defer tr.mu.Unlock()

	// Reset state for a fresh, non-additive derivation
	tr.tools = tr.tools[:0]
	tr.toolConfigs = make(map[string]musterMcp.ExecutionConfig)

	if len(conns) == 0 {
		return nil
	}

	// One slug per connection, assigned over the whole set (including APIs
	// whose spec fails below) so a transient fetch failure cannot renumber
	// another API's namespace.
	slugs := assignAPISlugs(conns)

	var allErrors []error
	apis := make([]convertedAPI, 0, len(conns))

	for i, conn := range conns {
		// Fetch the OpenAPI spec
		data, err := fetchSpecData(conn.SpecURL)
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("api %q: fetch spec: %w", conn.Name, err))
			continue
		}

		// Parse the spec under this connection's tolerance opt-in, the same
		// path connect/refresh/CLI-command generation use. A tolerated
		// connection whose spec fails a strict parse would otherwise vanish
		// from tools/list while its CLI commands still worked.
		fixed, tolerated, _, fixErr := app.ApplyTolerance(data, conn.Tolerate)
		if fixErr != nil {
			allErrors = append(allErrors, fmt.Errorf("api %q: %w", conn.Name, fixErr))
			continue
		}
		data = fixed

		parser := openapi.NewParser()
		result, err := parser.Parse(context.Background(), data, openapi.DefaultParseOptions())
		if err != nil {
			allErrors = append(allErrors, fmt.Errorf("api %q: parse spec: %w", conn.Name, err))
			continue
		}
		if len(tolerated) > 0 {
			fmt.Fprintf(os.Stderr, "⚠ %s: %d cosmetic spec defect(s) tolerated while loading MCP tools\n", conn.Name, len(tolerated))
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

		apis = append(apis, convertedAPI{
			conn:    conn,
			slug:    slugs[i],
			tools:   openAPITools,
			configs: configs,
		})
	}

	// Only return error if ALL APIs failed
	if len(apis) == 0 && len(allErrors) > 0 {
		return fmt.Errorf("all APIs failed to load: %v", allErrors)
	}

	tr.publishTools(apis)
	return nil
}

// publishTools fills tr.tools / tr.toolConfigs from the converted APIs.
// Callers must hold tr.mu for writing.
//
// Publication order: every namespaced tool first (connection order, then
// spec order), then the bare-name aliases of globally unique operations. The
// alias pass runs last so it can see every namespaced name and refuse to
// duplicate one.
func (tr *ToolRegistry) publishTools(apis []convertedAPI) {
	// How many APIs define each bare operation name.
	bareCounts := make(map[string]int)
	for _, api := range apis {
		for _, tool := range api.tools {
			bareCounts[tool.Name]++
		}
	}

	taken := make(map[string]struct{})
	for _, api := range apis {
		for _, tool := range api.tools {
			name := uniqueToolName(api.slug+toolNameSeparator+tool.Name, taken)

			hTool := tool.ToHandlersTool()
			hTool.Name = name
			hTool.Description = namespacedDescription(tool.Description, api.conn)

			tr.tools = append(tr.tools, hTool)
			tr.toolConfigs[name] = api.configs[tool.Name]
		}
	}

	for _, api := range apis {
		for _, tool := range api.tools {
			// The bare name survives only while it identifies exactly one
			// operation across every connected API, and only while no
			// namespaced name already claimed the string.
			if bareCounts[tool.Name] != 1 {
				continue
			}
			if _, claimed := taken[tool.Name]; claimed {
				continue
			}
			name := uniqueToolName(tool.Name, taken)

			hTool := tool.ToHandlersTool()
			hTool.Name = name

			tr.tools = append(tr.tools, hTool)
			tr.toolConfigs[name] = api.configs[tool.Name]
		}
	}
}

// assignAPISlugs returns one MCP-tool-name-safe slug per connection, in the
// given order, made unique by an "-N" suffix. Two connections can share a
// display name; without this, they would share a namespace and re-create the
// exact collision DF-039 removes.
func assignAPISlugs(conns []*app.APIConnection) []string {
	slugs := make([]string, len(conns))
	taken := make(map[string]struct{}, len(conns))

	for i, conn := range conns {
		slugs[i] = uniqueSlug(sanitizeAPISlug(conn), taken)
	}
	return slugs
}

// sanitizeAPISlug squeezes a connection name into [A-Za-z0-9_-] and caps its
// length. Characters that cannot appear in a tool name collapse into a single
// underscore, so "My API (v2)" becomes "My_API_v2" rather than losing the
// words that identify it.
func sanitizeAPISlug(conn *app.APIConnection) string {
	raw := conn.Name
	if raw == "" {
		raw = conn.ID
	}

	slug := toolNameDisallowed.ReplaceAllString(raw, "_")
	slug = strings.Trim(slug, "_")
	if len(slug) > maxAPISlugLen {
		slug = strings.Trim(slug[:maxAPISlugLen], "_")
	}
	if slug == "" {
		return fallbackAPISlug
	}
	return slug
}

// uniqueSlug returns base, or base-2, base-3, ... until the slug is unused.
// The suffix is reserved out of maxAPISlugLen so the result stays inside the
// slug budget.
func uniqueSlug(base string, taken map[string]struct{}) string {
	candidate := base
	for n := 2; ; n++ {
		if _, exists := taken[candidate]; !exists {
			taken[candidate] = struct{}{}
			return candidate
		}
		suffix := "-" + strconv.Itoa(n)
		if cut := maxAPISlugLen - len(suffix); len(base) > cut {
			candidate = strings.Trim(base[:cut], "_") + suffix
		} else {
			candidate = base + suffix
		}
	}
}

// uniqueToolName returns base, or base-2, base-3, ... until the name is
// unused, and records it. Names longer than maxToolNameLen are truncated
// first, keeping room for the suffix. This is a safety net: namespaced names
// are unique by construction, but a pathological connection name that already
// contains the separator must not be able to publish a duplicate.
func uniqueToolName(base string, taken map[string]struct{}) string {
	if len(base) > maxToolNameLen {
		base = base[:maxToolNameLen]
	}

	candidate := base
	for n := 2; ; n++ {
		if _, exists := taken[candidate]; !exists {
			taken[candidate] = struct{}{}
			return candidate
		}
		suffix := "-" + strconv.Itoa(n)
		if cut := maxToolNameLen - len(suffix); len(base) > cut {
			candidate = base[:cut] + suffix
		} else {
			candidate = base + suffix
		}
	}
}

// namespacedDescription labels a namespaced tool with the API it belongs to.
// The slug alone ("echo-2__listWidgets") does not tell a client which API that
// is; the tool's own description does, at no cost to its InputSchema.
func namespacedDescription(description string, conn *app.APIConnection) string {
	label := conn.Name
	if label == "" {
		label = conn.ID
	}
	if description == "" {
		return fmt.Sprintf("API %q", label)
	}
	return fmt.Sprintf("%s (API: %s)", description, label)
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
//
// Both published name forms resolve here: the namespaced
// "<slug>__<operationId>" always, and the bare operationId while it is
// globally unique. Each name maps to exactly one API's config, so a call can
// never be routed to another connected API's base URL (DF-039).
func (tr *ToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	tr.mu.RLock()
	config, ok := tr.toolConfigs[name]
	tr.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}

	result, err := executeHTTPUnderRetryPin(config, args)
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
		defer func() { _ = resp.Body.Close() }()
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
