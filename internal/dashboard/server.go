// Package dashboard provides the web dashboard HTTP server for MusterFlow.
package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
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
	token         string
	flowsDir      string
	mux           *http.ServeMux
	mcpHandler    http.Handler
	metrics       *Metrics
	handler       http.Handler
}

// NewServer creates a dashboard server with bearer auth disabled. It is the
// in-process/embedding constructor: the address is normalized like
// NewServerWithToken's (a host-less ":port" becomes loopback) but exposure is
// NOT validated, because this signature cannot report an error. Production
// startup goes through NewServerWithToken, which refuses a non-loopback bind
// with no token.
func NewServer(registry *app.Registry, catalogClient *catalog.Client, toolRegistry *mcp.ToolRegistry, addr string) *Server {
	return newServer(registry, catalogClient, toolRegistry, addr, "", false)
}

// NewServerWithToken creates a dashboard server that requires
// "Authorization: Bearer <token>" on /api/* (except /api/health), /mcp and
// /hooks/* whenever token is non-empty.
//
// It fails closed: a non-loopback address with no token is refused, because
// that combination publishes the registry, flow sources and an unauthenticated
// webhook trigger to every host that can reach the port. The returned error
// tells the operator both ways out (set dashboard_token, or bind loopback).
func NewServerWithToken(registry *app.Registry, catalogClient *catalog.Client, toolRegistry *mcp.ToolRegistry, addr, token string) (*Server, error) {
	resolved, err := ListenAddr(addr, 0)
	if err != nil {
		return nil, err
	}
	if token == "" && !IsLoopbackAddr(resolved) {
		return nil, fmt.Errorf(
			"dashboard: refusing to listen on %s without a dashboard token — "+
				"set dashboard_token in config.yaml (or MUSTERFLOW_DASHBOARD_TOKEN) to require a bearer token, "+
				"or bind loopback (--dashboard-addr 127.0.0.1:<port>)",
			resolved)
	}
	return newServer(registry, catalogClient, toolRegistry, resolved, token, true), nil
}

// newServer builds the server. warn adds the one-line startup notice when the
// dashboard is reachable without a token; it is only requested by the
// production constructor so tests stay quiet.
func newServer(registry *app.Registry, catalogClient *catalog.Client, toolRegistry *mcp.ToolRegistry, addr, token string, warn bool) *Server {
	if resolved, err := ListenAddr(addr, 0); err == nil {
		addr = resolved
	}
	s := &Server{
		registry:      registry,
		catalogClient: catalogClient,
		toolRegistry:  toolRegistry,
		addr:          addr,
		token:         token,
		flowsDir:      filepath.Join(registry.DataDir(), "flows"),
		mux:           http.NewServeMux(),
		metrics:       NewMetrics(),
	}
	s.registerRoutes()
	s.handler = s.metrics.Instrument(s.mux)
	if warn && token == "" && IsLoopbackAddr(addr) {
		fmt.Fprintln(os.Stderr, AuthDisabledWarning)
	}
	return s
}

// Metrics returns the collector this server exports on /metrics. It is exposed
// so an embedding process can reuse or assert on the same counters.
func (s *Server) Metrics() *Metrics {
	return s.metrics
}

// SetMCPHandler sets the HTTP handler for the /mcp JSON-RPC endpoint.
// If not set, /mcp returns a JSON-RPC error indicating no API is connected.
func (s *Server) SetMCPHandler(h http.Handler) {
	s.mcpHandler = h
}

func (s *Server) registerRoutes() {
	// /api/health and /metrics are deliberately unauthenticated even when a
	// dashboard token is configured:
	//   - /api/health is the liveness probe the CLI uses to detect a running
	//     dashboard (isMusterflowDashboard) and the port-selection walk uses;
	//     a probe cannot hold a secret, and the payload is a status string plus
	//     a connection count.
	//   - /metrics carries Prometheus counters (method, path, code) and no
	//     registry, flow or definition data.
	// Everything that can read the registry, list flow sources, export
	// definitions, run a flow or trigger outbound HTTP requires the bearer
	// token whenever one is configured.
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/apis", s.requireAuth(s.handleAPIs))
	s.mux.HandleFunc("/api/apis/", s.requireAuth(s.handleAPIByID))
	s.mux.HandleFunc("/api/flows", s.requireAuth(s.handleFlows))
	s.mux.HandleFunc("/api/flows/", s.requireAuth(s.handleFlowRun))
	s.mux.HandleFunc("/api/catalog/search", s.requireAuth(s.handleCatalogSearch))
	s.mux.HandleFunc("/api/mcp/info", s.requireAuth(s.handleMCPInfo))
	s.mux.HandleFunc("/api/export", s.requireAuth(s.handleExport))
	s.mux.HandleFunc("/api/import", s.requireAuth(s.handleImport))
	s.mux.HandleFunc("/mcp", s.requireAuth(s.handleMCP))
	s.mux.HandleFunc("/hooks/", s.requireAuth(s.handleWebhook))
	s.mux.HandleFunc("/metrics", s.metrics.HandleMetrics)
	// REVIEW-MUSTERFLOW-20261121-API-CATCHALL: any unmatched /api/* path must
	// answer a stable JSON 404 instead of falling through to the "/" catch-all
	// and serving dashboard HTML to API clients. The "/api" exact pattern also
	// lands here so a bare /api gets the JSON 404 (without it, ServeMux would
	// redirect /api to /api/). More specific registrations above (e.g.
	// /api/health, /api/apis/) always win over these subtree patterns.
	s.mux.HandleFunc("/api", s.handleAPINotFound)
	s.mux.HandleFunc("/api/", s.handleAPINotFound)
	s.mux.HandleFunc("/", serveIndex)
}

// handleAPINotFound answers requests to /api paths that match no registered
// API route with the same JSON error shape the other handlers use
// ({"error": "..."} via writeJSON), never the dashboard HTML.
func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// baseURL is the address the dashboard's own flows call back on (webhook URLs,
// MCP endpoint and flow HTTP calls). It renders the loopback bind as
// http://localhost:<port> so the advertised URLs stay stable for local use.
func (s *Server) baseURL() string {
	return BaseURL(s.addr)
}

// refreshMCPTools re-derives the MCP tool registry from the current set of
// connected APIs. Every handler that mutates the registry (connect, refresh,
// disconnect, import) must call it, so MCP tools/list tracks the registry
// without a server restart — the README's dynamic-tools promise (DF-015 for
// the additive paths, DF-029 for disconnect).
//
// Failures are non-fatal: the registry mutation has already succeeded and the
// next mutation re-runs the refresh. A stale tool list is reported loudly on
// stderr rather than turned into an error response the caller cannot act on.
func (s *Server) refreshMCPTools() {
	if s.toolRegistry == nil {
		return
	}
	if err := s.toolRegistry.Refresh(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: MCP tool refresh: %v\n", err)
	}
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
	return http.ListenAndServe(s.addr, s.handler)
}

// Handler returns the http.Handler for embedding in another server. The handler
// is the instrumented mux, so embedding code exports the same metrics.
func (s *Server) Handler() http.Handler {
	return s.handler
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

	// DF-015: re-derive the MCP tools so the new API's tools appear in
	// tools/list without a server restart.
	s.refreshMCPTools()

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
		conn, err := s.registry.GetByNameOrID(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, conn)
	case http.MethodDelete:
		// Resolve name-or-id so delete-by-name also works.
		conn, err := s.registry.GetByNameOrID(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		if err := s.registry.Remove(conn.ID); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		// DF-029: re-derive the MCP tools so the removed API's tools stop
		// being served by tools/list (and its execution configs disappear)
		// in this same process, without a server restart.
		s.refreshMCPTools()
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": conn.ID})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleRefreshAPI handles POST /api/apis/<id-or-name>/refresh
func (s *Server) handleRefreshAPI(w http.ResponseWriter, r *http.Request, apiID string) {
	// Resolve name-or-id so refresh-by-name also works.
	conn, err := s.registry.GetByNameOrID(apiID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	result, err := app.Refresh(r.Context(), s.registry, conn.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// DF-015: re-derive the MCP tools so the refreshed API's updated tools
	// appear in tools/list without a server restart.
	s.refreshMCPTools()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":            result.Connection.Name,
		"old_version":     result.OldVersion,
		"new_version":     result.NewVersion,
		"version_changed": result.VersionChanged,
		"old_endpoints":   result.OldEndpoints,
		"new_endpoints":   result.NewEndpoints,
		"tolerated":       result.Tolerated,
		"repaired":        result.Repaired,
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
		// DF-035: a backend-absent catalog is a client-visible not-found, not a
		// server error. Passing the class through keeps the same operation
		// answering the same way with or without the dashboard: status 404 and
		// the client's own message text ("catalog backend not available
		// (HTTP 404): catalog repo … has no entries.json or index.json — repo
		// absent or private"), which is exactly what the local CLI path prints.
		//
		// Everything else stays 500 on purpose: a transport failure
		// ("fetch catalog index: …"), a non-200 upstream status
		// ("catalog returned status 503 for <url>") or an undecodable index
		// ("decode catalog: …") means the dashboard could not produce an
		// answer, which is the 500-class this endpoint already advertises.
		status := http.StatusInternalServerError
		if errors.Is(err, catalog.ErrBackendUnavailable) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
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
	endpoint := s.baseURL() + "/mcp"

	if s.toolRegistry == nil {
		apiCount := 0
		if s.registry != nil {
			apiCount = len(s.registry.List())
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"endpoint":   endpoint,
			"transport":  "HTTP JSON-RPC 2.0",
			"tool_count": 0,
			"api_count":  apiCount,
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

	apiCount := 0
	if s.registry != nil {
		apiCount = len(s.registry.List())
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"endpoint":   endpoint,
		"transport":  "HTTP JSON-RPC 2.0",
		"tool_count": len(toolInfos),
		"api_count":  apiCount,
		"tools":      toolInfos,
	})
}

// handleExport handles GET /api/export — streams all API connections as
// JSONL (one JSON object per line, Content-Type application/x-ndjson).
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		store := s.registry.Store()
		if store == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registry not loaded"})
			return
		}
		conns, err := store.List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		for _, conn := range conns {
			if err := enc.Encode(conn); err != nil {
				return // client may have disconnected
			}
		}
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleImport handles POST /api/import — reads JSONL from the request body
// and imports all connections into the store. Existing connections with the
// same ID are replaced. Responds with {"imported": N}.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		store := s.registry.Store()
		if store == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "registry not loaded"})
			return
		}
		if r.Body == nil || r.ContentLength == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing request body"})
			return
		}
		n, err := app.ImportJSONLReader(store, r.Body)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// DF-029: import replaces/inserts connections directly in the store,
		// so the MCP tools must be re-derived here too — otherwise imported
		// APIs' tools never appear until a restart (and replaced ones keep
		// serving their old definitions).
		s.refreshMCPTools()
		writeJSON(w, http.StatusOK, map[string]interface{}{"imported": n})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
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

// handleFlows handles GET /api/flows (list) and POST /api/flows (create).
func (s *Server) handleFlows(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		engine := workflow.NewEngine(s.flowsDir, s.baseURL())
		flows, err := engine.List()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if flows == nil {
			flows = []workflow.Flow{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"flows": flows})
	case http.MethodPost:
		s.handleFlowCreate(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleFlowCreate handles POST /api/flows to create a new flow.
// Body: {"name": "...", "source": "...", "description": "...", "webhook": bool}
func (s *Server) handleFlowCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		Description string `json:"description"`
		Webhook     bool   `json:"webhook"`
	}
	if r.Body == nil || r.ContentLength == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing request body"})
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if body.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing flow name"})
		return
	}

	engine := workflow.NewEngine(s.flowsDir, s.baseURL())
	flow, err := engine.Create(body.Name, body.Source, body.Description, body.Webhook)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"name":        flow.Name,
		"source":      flow.Source,
		"description": flow.Description,
		"webhook":     flow.Webhook,
		"webhook_url": flow.WebhookURL,
		"flows_dir":   s.flowsDir,
	})
}

// handleFlowRun handles POST /api/flows/<name>/run to execute a flow.
// Optional JSON body: {"trigger": {...}}
func (s *Server) handleFlowRun(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/api/flows/"):]
	// Require the /run suffix
	if !strings.HasSuffix(name, "/run") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown flow endpoint"})
		return
	}
	name = strings.TrimSuffix(name, "/run")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing flow name"})
		return
	}

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var payload map[string]interface{}
	if r.Body != nil && r.ContentLength != 0 {
		var body struct {
			Trigger map[string]interface{} `json:"trigger"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		payload = body.Trigger
	}

	engine := workflow.NewEngine(s.flowsDir, s.baseURL())
	output, err := engine.Run(name, payload)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"result": output})
}

// handleWebhook handles /hooks/<name>: a webhook trigger for a flow.
//
// POST only. Every other method is refused with 405 before an engine is even
// constructed, so a bare GET — which the route used to accept, running the flow
// and returning its output — cannot trigger arbitrary outbound HTTP on behalf
// of anyone who can reach the port.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// Extract flow name from path: /hooks/<name>
	name := r.URL.Path[len("/hooks/"):]
	if name == "" {
		writeJSON(w, 400, map[string]string{"error": "missing flow name"})
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var payload map[string]interface{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&payload)
	}

	engine := workflow.NewEngine(s.flowsDir, s.baseURL())
	output, err := engine.Run(name, payload)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, 200, map[string]string{"result": output})
}
