// Package mcp provides the HTTP-based MCP (Model Context Protocol) server
// that bridges muster's JSON-RPC handler registry to HTTP transport.
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/wojons/muster/pkg/mcp/handlers"
)

// rpcRequest is a JSON-RPC 2.0 request (local type to avoid coupling
// to muster's stdio server types).
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// HTTPServer bridges muster's MCP handler registry to HTTP transport.
// It accepts JSON-RPC 2.0 POST requests and dispatches them to the
// registered handlers.
type HTTPServer struct {
	registry *handlers.Registry
}

// NewHTTPServer creates a new HTTP MCP server backed by the given handler registry.
func NewHTTPServer(registry *handlers.Registry) *HTTPServer {
	return &HTTPServer{registry: registry}
}

// ServeHTTP implements http.Handler. It accepts only POST requests,
// parses the JSON-RPC 2.0 body, dispatches to the handler registry,
// and returns a JSON-RPC 2.0 response.
func (s *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(rpcResponse{
			JSONRPC: "2.0",
			ID:      nil,
			Error: &rpcError{
				Code:    -32000,
				Message: "method not allowed: use POST",
			},
		})
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	result, err := s.registry.Handle(r.Context(), req.Method, req.Params)
	if err != nil {
		// Check for MCPError to extract code/message
		if mcpErr, ok := err.(*handlers.MCPError); ok {
			writeRPCError(w, req.ID, mcpErr.Code, mcpErr.Message)
			return
		}
		// Generic error → -32000
		writeRPCError(w, req.ID, -32000, err.Error())
		return
	}

	// Notifications (id is nil/absent) get no response per JSON-RPC spec,
	// but we still return a response for robustness — MCP clients vary.
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
	// If result is nil (e.g. notification), omit it from JSON
	if result == nil {
		resp.Result = nil
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Best-effort: can't change status code after header sent
		_ = fmt.Errorf("encode rpc response: %w", err)
	}
}

// writeRPCError writes a JSON-RPC error response.
func writeRPCError(w http.ResponseWriter, id interface{}, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	})
}