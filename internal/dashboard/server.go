// Package dashboard provides the web dashboard HTTP server for MusterFlow.
package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/catalog"
	"github.com/totalwindupflightsystems/musterflow/internal/mcp"
	"github.com/totalwindupflightsystems/musterflow/internal/workflow"
)

// Server serves the MusterFlow web dashboard.
type Server struct {
	registry      *app.Registry
	catalogClient *catalog.Client
	toolRegistry  *mcp.ToolRegistry
	addr          string
	mux           *http.ServeMux
	mcpHandler    http.Handler
}

// NewServer creates a new dashboard server.
func NewServer(registry *app.Registry, catalogClient *catalog.Client, toolRegistry *mcp.ToolRegistry, addr string) *Server {
	s := &Server{
		registry:      registry,
		catalogClient: catalogClient,
		toolRegistry:  toolRegistry,
		addr:          addr,
		mux:           http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

// SetMCPHandler sets the HTTP handler for the /mcp JSON-RPC endpoint.
// If not set, /mcp returns a JSON-RPC error indicating no API is connected.
func (s *Server) SetMCPHandler(h http.Handler) {
	s.mcpHandler = h
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/apis", s.handleAPIs)
	s.mux.HandleFunc("/api/apis/", s.handleAPIByID)
	s.mux.HandleFunc("/api/catalog/search", s.handleCatalogSearch)
	s.mux.HandleFunc("/api/mcp/info", s.handleMCPInfo)
	s.mux.HandleFunc("/mcp", s.handleMCP)
	s.mux.HandleFunc("/hooks/", s.handleWebhook)
	s.mux.HandleFunc("/", serveIndex)
}

// handleMCP dispatches to the MCP handler if configured, otherwise returns
// a JSON-RPC error indicating the MCP server is not configured.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if s.mcpHandler != nil {
		s.mcpHandler.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      nil,
		"error": map[string]interface{}{
			"code":    -32000,
			"message": "MCP server not configured — connect an API first",
		},
	})
}

// Run starts the HTTP server. Blocks until the server exits.
func (s *Server) Run() error {
	return http.ListenAndServe(s.addr, s.mux)
}

// Handler returns the http.Handler for embedding in another server.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "ok",
		"connected_apis": len(s.registry.List()),
	})
}

func (s *Server) handleAPIs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"apis": s.registry.List(),
		})
	case http.MethodPost:
		// POST with nil/empty body falls through to method-not-allowed
		// to preserve backward compatibility with clients that don't send a body.
		if r.Body == nil || r.ContentLength == 0 {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		s.handleAPIAdd(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleAPIAdd connects a new API via the dashboard HTTP API.
// POST /api/apis with JSON body: {spec_url, base_url, name, auth_type}
func (s *Server) handleAPIAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SpecURL  string `json:"spec_url"`
		BaseURL  string `json:"base_url"`
		Name     string `json:"name"`
		AuthType string `json:"auth_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	result, err := app.Connect(r.Context(), s.registry, app.ConnectOptions{
		SpecURL:  body.SpecURL,
		BaseURL:  body.BaseURL,
		Name:     body.Name,
		AuthType: body.AuthType,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":             result.Connection.ID,
		"name":           result.Connection.Name,
		"spec_title":     result.SpecTitle,
		"spec_version":   result.SpecVersion,
		"endpoint_count": result.EndpointCount,
		"base_url":       result.Connection.BaseURL,
	})
}

func (s *Server) handleAPIByID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/apis/"):]
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing api id"})
		return
	}

	// Check for /refresh suffix
	if strings.HasSuffix(path, "/refresh") && r.Method == http.MethodPost {
		id := strings.TrimSuffix(path, "/refresh")
		s.handleRefreshAPI(w, r, id)
		return
	}

	id := path

	switch r.Method {
	case http.MethodGet:
		conn, err := s.registry.Get(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, conn)
	case http.MethodDelete:
		if err := s.registry.Remove(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleRefreshAPI handles POST /api/apis/<id>/refresh
func (s *Server) handleRefreshAPI(w http.ResponseWriter, r *http.Request, apiID string) {
	result, err := app.Refresh(r.Context(), s.registry, apiID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":            result.Connection.Name,
		"old_version":     result.OldVersion,
		"new_version":     result.NewVersion,
		"version_changed": result.VersionChanged,
		"old_endpoints":   result.OldEndpoints,
		"new_endpoints":   result.NewEndpoints,
	})
}

// handleCatalogSearch searches the community catalog.
// GET /api/catalog/search?q=<query>
func (s *Server) handleCatalogSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"results": []catalog.CatalogEntry{},
			"total":   0,
		})
		return
	}

	entries, err := s.catalogClient.Search(query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []catalog.CatalogEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results": entries,
		"total":   len(entries),
	})
}

// mcpToolInfo is the JSON shape returned for each tool in /api/mcp/info.
type mcpToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

// handleMCPInfo returns information about the MCP endpoint and its tools.
// GET /api/mcp/info
func (s *Server) handleMCPInfo(w http.ResponseWriter, r *http.Request) {
	endpoint := fmt.Sprintf("http://localhost%s/mcp", s.addr)

	if s.toolRegistry == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"endpoint":   endpoint,
			"transport":  "HTTP JSON-RPC 2.0",
			"tool_count": 0,
			"tools":      []mcpToolInfo{},
		})
		return
	}

	tools := s.toolRegistry.ListTools()
	toolInfos := make([]mcpToolInfo, 0, len(tools))
	for _, t := range tools {
		example := buildToolExample(t.Name, t.InputSchema)
		toolInfos = append(toolInfos, mcpToolInfo{
			Name:        t.Name,
			Description: t.Description,
			Example:     example,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"endpoint":   endpoint,
		"transport":  "HTTP JSON-RPC 2.0",
		"tool_count": len(toolInfos),
		"tools":      toolInfos,
	})
}

// buildToolExample generates a copy-pasteable JSON-RPC tools/call example.
// If the input schema can be parsed, we extract property names and provide
// placeholder values; otherwise we emit an empty arguments object.
func buildToolExample(toolName string, inputSchema json.RawMessage) string {
	args := buildExampleArgs(inputSchema)
	example := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}
	b, err := json.MarshalIndent(example, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, toolName)
	}
	return string(b)
}

// buildExampleArgs extracts property names from a JSON Schema input schema
// and builds a placeholder arguments object with example values.
func buildExampleArgs(inputSchema json.RawMessage) map[string]interface{} {
	args := map[string]interface{}{}
	if len(inputSchema) == 0 {
		return args
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(inputSchema, &schema); err != nil {
		return args
	}
	for name, prop := range schema.Properties {
		args[name] = exampleValueForType(prop.Type)
	}
	return args
}

// exampleValueForType returns a placeholder value for a JSON Schema type.
func exampleValueForType(t string) interface{} {
	switch t {
	case "string":
		return "value"
	case "integer", "number":
		return 1
	case "boolean":
		return false
	case "array":
		return []interface{}{}
	case "object":
		return map[string]interface{}{}
	default:
		return "value"
	}
}

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// Extract flow name from path: /hooks/<name>
	name := r.URL.Path[len("/hooks/"):]
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "missing flow name"})
		return
	}

	var payload map[string]interface{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}

	engine := workflow.NewEngine(
		filepath.Join(app.DefaultDataDir(), "flows"),
		fmt.Sprintf("http://localhost%s", s.addr),
	)
	output, err := engine.Run(name, payload)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, map[string]string{"result": output})
}
