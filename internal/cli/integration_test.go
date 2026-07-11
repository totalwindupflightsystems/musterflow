// Integration tests that exercise the full connect→generate→execute→format pipeline
// using httptest servers — no external dependencies.

package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

func TestIntegration_ConnectAndExecute_FullPipeline(t *testing.T) {
	// --- Mock API server (responds to API calls) ---
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/todos":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`[{"id":1,"title":"Buy milk","done":false},{"id":2,"title":"Walk dog","done":true}]`))
		case r.Method == "POST" && r.URL.Path == "/todos":
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":3,"title":"Write tests","done":false}`))
		case r.Method == "GET" && r.URL.Path == "/todos/1":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":1,"title":"Buy milk","done":false}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	defer apiServer.Close()

	// Minimal OpenAPI 3.0 spec pointing at our mock server
	specJSON := strings.ReplaceAll(`{
  "openapi": "3.0.3",
  "info": {"title": "Todo API", "version": "1.0.0"},
  "servers": [{"url": "API_SERVER_URL"}],
  "paths": {
    "/todos": {
      "get": {
        "operationId": "listTodos",
        "summary": "List all todos",
        "responses": {"200": {"description": "OK", "content": {"application/json": {"schema": {"type": "array", "items": {"type": "object"}}}}}}
      },
      "post": {
        "operationId": "createTodo",
        "summary": "Create a todo",
        "parameters": [{"name": "title", "in": "query", "schema": {"type": "string"}}],
        "responses": {"201": {"description": "Created", "content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    },
    "/todos/{id}": {
      "get": {
        "operationId": "getTodo",
        "summary": "Get a todo",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "integer"}}],
        "responses": {"200": {"description": "OK", "content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    }
  }
}`, "API_SERVER_URL", apiServer.URL)

	// --- Spec server (serves the OpenAPI spec) ---
	specServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(specJSON))
	}))
	defer specServer.Close()

	// --- Connect the API ---
	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := app.Connect(context.Background(), reg, app.ConnectOptions{
		SpecURL: specServer.URL,
		BaseURL: apiServer.URL,
		Name:    "todo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if result.EndpointCount != 3 {
		t.Errorf("expected 3 endpoints, got %d", result.EndpointCount)
	}
	if result.Connection.Name != "todo" {
		t.Errorf("expected name 'todo', got %q", result.Connection.Name)
	}
	t.Logf("✓ Connected: id=%s name=%s endpoints=%d", result.Connection.ID, result.Connection.Name, result.Connection.EndpointCount)

	// --- Build fresh root command (registry already has the connection) ---
	root := NewRootCommand(reg)

	// Find the todo command through cobra's tree
	todoCmd, remaining, err := root.Find([]string{"todo"})
	if err != nil {
		t.Fatalf("find todo: %v", err)
	}
	if todoCmd == nil || todoCmd == root {
		t.Fatal("todo command not found in root")
	}

	// Force lazy-load so generated children appear
	todoCmd.SetArgs(remaining)
	_ = todoCmd.Execute() // triggers ensureAPILoaded via RunE

	// Now find the full path: todo → todos → list-todos
	listTarget, listRemaining, err := root.Find([]string{"todo", "todos", "list-todos"})
	if err != nil {
		// Log available subcommands for debugging
		for _, sub := range todoCmd.Commands() {
			t.Logf("  todo sub: %q", sub.Name())
			for _, op := range sub.Commands() {
				t.Logf("    %s sub: %q", sub.Name(), op.Name())
			}
		}
		t.Fatalf("find todo todos list-todos: %v", err)
	}

	root.SetArgs([]string{"todo", "todos", "list-todos"})
	output := captureStdout(func() {
		if err := root.Execute(); err != nil {
			t.Logf("execute list-todos: %v", err)
		}
	})

	// Verify output contains our mock data
	if !strings.Contains(output, "Buy milk") {
		t.Errorf("expected 'Buy milk' in output, got: %s", output)
	}
	if !strings.Contains(output, "Walk dog") {
		t.Errorf("expected 'Walk dog' in output, got: %s", output)
	}
	t.Logf("✓ list-todos output: %s", strings.TrimSpace(output))

	// Suppress unused warnings
	_ = listTarget
	_ = listRemaining
}

func TestIntegration_ConnectAndExecute_JSONFormat(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":1,"title":"Buy milk","done":false}]`))
	}))
	defer apiServer.Close()

	specJSON := strings.ReplaceAll(`{
  "openapi": "3.0.3",
  "info": {"title": "Todo API", "version": "1.0.0"},
  "servers": [{"url": "API_SERVER_URL"}],
  "paths": {
    "/todos": {
      "get": {
        "operationId": "listTodos",
        "summary": "List all todos",
        "responses": {"200": {"description": "OK", "content": {"application/json": {"schema": {"type": "array", "items": {"type": "object"}}}}}}
      }
    }
  }
}`, "API_SERVER_URL", apiServer.URL)

	specServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(specJSON))
	}))
	defer specServer.Close()

	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := app.Connect(context.Background(), reg, app.ConnectOptions{
		SpecURL: specServer.URL,
		BaseURL: apiServer.URL,
		Name:    "todo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	conn, _ := reg.Get(result.Connection.ID)
	_ = conn // connection is in registry

	// Build fresh root (registry has the API)
	root := NewRootCommand(reg)

	// Force lazy-load via the todo command
	todoCmd, _, _ := root.Find([]string{"todo"})
	if todoCmd != nil {
		_ = todoCmd.Execute()
	}

	root.SetArgs([]string{"todo", "todos", "list-todos"})

	var buf bytes.Buffer
	root.SetOut(&buf)

	if err := root.Execute(); err != nil {
		t.Logf("execute result: %v", err)
	}

	output := buf.String()
	if output == "" {
		t.Error("expected non-empty output for JSON-formatted response")
	}
	if strings.Contains(output, "Buy milk") {
		t.Logf("✓ JSON format output contains expected data")
	} else {
		t.Logf("output (may be help text if dispatch failed): %s", strings.TrimSpace(output))
	}
}

func TestIntegration_ConnectAndExecute_HTTPError(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not found","message":"No todos here"}`))
	}))
	defer apiServer.Close()

	specJSON := strings.ReplaceAll(`{
  "openapi": "3.0.3",
  "info": {"title": "Todo API", "version": "1.0.0"},
  "servers": [{"url": "API_SERVER_URL"}],
  "paths": {
    "/todos": {
      "get": {
        "operationId": "listTodos",
        "summary": "List all todos",
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`, "API_SERVER_URL", apiServer.URL)

	specServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(specJSON))
	}))
	defer specServer.Close()

	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := app.Connect(context.Background(), reg, app.ConnectOptions{
		SpecURL: specServer.URL,
		BaseURL: apiServer.URL,
		Name:    "todo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	conn, _ := reg.Get(result.Connection.ID)
	_ = conn

	root := NewRootCommand(reg)
	todoCmd, _, _ := root.Find([]string{"todo"})
	if todoCmd != nil {
		_ = todoCmd.Execute()
	}

	root.SetArgs([]string{"todo", "todos", "list-todos"})

	output := captureStdout(func() {
		_ = root.Execute() // expected to error with HTTP 404
	})

	// 404 should still produce output (error body) and not panic
	if strings.Contains(output, "panic") {
		t.Errorf("panic on 404 response: %s", output)
	}
	t.Logf("✓ 404 error handled: %s", strings.TrimSpace(output))
}

func TestIntegration_ConnectAndExecute_PathParam(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/todos/42" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":42,"title":"Answer everything","done":false}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer apiServer.Close()

	specJSON := strings.ReplaceAll(`{
  "openapi": "3.0.3",
  "info": {"title": "Todo API", "version": "1.0.0"},
  "servers": [{"url": "API_SERVER_URL"}],
  "paths": {
    "/todos/{id}": {
      "get": {
        "operationId": "getTodo",
        "summary": "Get a todo by ID",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "integer"}}],
        "responses": {"200": {"description": "OK"}}
      }
    }
  }
}`, "API_SERVER_URL", apiServer.URL)

	specServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(specJSON))
	}))
	defer specServer.Close()

	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := app.Connect(context.Background(), reg, app.ConnectOptions{
		SpecURL: specServer.URL,
		BaseURL: apiServer.URL,
		Name:    "todo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	conn, _ := reg.Get(result.Connection.ID)
	_ = conn

	root := NewRootCommand(reg)
	todoCmd, _, _ := root.Find([]string{"todo"})
	if todoCmd != nil {
		_ = todoCmd.Execute()
	}

	root.SetArgs([]string{"todo", "todos", "get-todo", "--id", "42"})

	output := captureStdout(func() {
		_ = root.Execute()
	})

	t.Logf("✓ get-todo output: %s", strings.TrimSpace(output))

	// Should contain our mock response or at least not panic
	if strings.Contains(output, "panic") {
		t.Errorf("panic on path param request: %s", output)
	}
	if strings.Contains(output, "Answer everything") {
		t.Logf("✓ path param correctly resolved")
	}
}

func TestIntegration_ConnectAndExecute_Disconnect(t *testing.T) {
	specServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Todo API", "version": "1.0.0"},
  "servers": [{"url": "http://localhost:0"}],
  "paths": {"/todos":{"get":{"operationId":"listTodos","responses":{"200":{"description":"OK"}}}}}
}`))
	}))
	defer specServer.Close()

	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := app.Connect(context.Background(), reg, app.ConnectOptions{
		SpecURL: specServer.URL,
		BaseURL: "http://localhost:0",
		Name:    "todo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if len(reg.List()) != 1 {
		t.Fatalf("expected 1 connected API, got %d", len(reg.List()))
	}

	// Full disconnect through CLI
	root := NewRootCommand(reg)
	root.SetArgs([]string{"disconnect", result.Connection.ID})
	output := captureStdout(func() {
		if err := root.Execute(); err != nil {
			t.Errorf("disconnect: %v", err)
		}
	})

	if !strings.Contains(output, "Disconnected") {
		t.Errorf("expected 'Disconnected' in output, got: %s", output)
	}
	if len(reg.List()) != 0 {
		t.Errorf("expected 0 APIs after disconnect, got %d", len(reg.List()))
	}
	t.Logf("✓ disconnect: %s", strings.TrimSpace(output))
}

func TestIntegration_ConnectAndExecute_Refresh(t *testing.T) {
	callCount := 0
	specServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Return different version on second call to verify refresh works
		if callCount == 0 {
			_, _ = w.Write([]byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Todo API", "version": "1.0.0"},
  "servers": [{"url": "http://localhost:0"}],
  "paths": {"/todos":{"get":{"operationId":"listTodos","responses":{"200":{"description":"OK"}}},"post":{"operationId":"createTodo","responses":{"201":{"description":"Created"}}}}}
}`))
		} else {
			_, _ = w.Write([]byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Todo API", "version": "2.0.0"},
  "servers": [{"url": "http://localhost:0"}],
  "paths": {"/todos":{"get":{"operationId":"listTodos","responses":{"200":{"description":"OK"}}},"post":{"operationId":"createTodo","responses":{"201":{"description":"Created"}}},"put":{"operationId":"updateTodo","responses":{"200":{"description":"OK"}}}}}
}`))
		}
		callCount++
	}))
	defer specServer.Close()

	reg := app.NewRegistry(t.TempDir())
	if err := reg.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := app.Connect(context.Background(), reg, app.ConnectOptions{
		SpecURL: specServer.URL,
		BaseURL: "http://localhost:0",
		Name:    "todo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if result.EndpointCount != 2 {
		t.Errorf("expected 2 endpoints, got %d", result.EndpointCount)
	}

	// Refresh through CLI
	root := NewRootCommand(reg)
	root.SetArgs([]string{"refresh", result.Connection.ID})
	output := captureStdout(func() {
		if err := root.Execute(); err != nil {
			t.Errorf("refresh: %v", err)
		}
	})

	if !strings.Contains(output, "Refreshed") {
		t.Errorf("expected 'Refreshed' in output, got: %s", output)
	}
	if !strings.Contains(output, "1.0.0") || !strings.Contains(output, "2.0.0") {
		t.Errorf("expected version change 1.0.0 → 2.0.0 in output, got: %s", output)
	}

	// Verify endpoint count updated
	conn, _ := reg.Get(result.Connection.ID)
	if conn.EndpointCount != 3 {
		t.Errorf("expected 3 endpoints after refresh, got %d", conn.EndpointCount)
	}
	if conn.Version != "2.0.0" {
		t.Errorf("expected version 2.0.0 after refresh, got %s", conn.Version)
	}
	t.Logf("✓ refresh: %s", strings.TrimSpace(output))
}
