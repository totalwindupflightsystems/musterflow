// Package dashboard provides the web dashboard HTTP server for MusterFlow.
package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// Server serves the MusterFlow web dashboard.
type Server struct {
	registry  *app.Registry
	addr      string
	mux       *http.ServeMux
	mcpHandler http.Handler
}

// NewServer creates a new dashboard server.
func NewServer(registry *app.Registry, addr string) *Server {
	s := &Server{
		registry: registry,
		addr:     addr,
		mux:      http.NewServeMux(),
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
	s.mux.HandleFunc("/mcp", s.handleMCP)
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
	json.NewEncoder(w).Encode(map[string]interface{}{
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
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleAPIByID(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/apis/"):]
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing api id"})
		return
	}

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

func writeJSON(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}
