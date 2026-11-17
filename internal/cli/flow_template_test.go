package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
	"github.com/totalwindupflightsystems/musterflow/internal/workflow"
)

// DF-036: `flow create` without --source must ship a runnable, commented DSL
// template instead of a bare placeholder. These tests pin both create paths
// (local engine + dashboard payload) and prove the stored template actually
// compiles and runs through the workflow engine.

// placeholderFlowSource is the pre-DF-036 template. It parsed (a comment is
// valid Starlark) but taught the author nothing, so no create path may write
// it any more.
const placeholderFlowSource = "# Write your Starlark workflow here\n"

func TestFlowCreateWithoutSourceStoresDefaultTemplate(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	root := NewRootCommand(r)
	root.SetOut(new(strings.Builder))
	root.SetArgs([]string{"flow", "create", "demo"})
	if err := root.Execute(); err != nil {
		t.Fatalf("flow create demo: %v", err)
	}

	starPath := filepath.Join(r.DataDir(), "flows", "demo.star")
	data, err := os.ReadFile(starPath)
	if err != nil {
		t.Fatalf("read stored flow %s: %v", starPath, err)
	}
	stored := string(data)

	if stored == placeholderFlowSource {
		t.Fatal("flow create still stores the bare placeholder template")
	}
	if stored != defaultFlowSource {
		t.Fatalf("stored template differs from defaultFlowSource\n--- got ---\n%s\n--- want ---\n%s", stored, defaultFlowSource)
	}

	// The template's job is guidance: name every predeclared global a flow may
	// use, show trigger handling, and state the top-level statement restriction.
	for _, want := range []string{
		"trigger",
		"print",
		"http_get",
		"http_post",
		"http_request",
		"def main():",
		`if trigger == None:`,
		"fails to compile",
	} {
		if !strings.Contains(stored, want) {
			t.Errorf("template does not mention %q:\n%s", want, stored)
		}
	}

	// Readability budget from the row: keep the printed/stored template short.
	if lines := strings.Count(strings.TrimRight(stored, "\n"), "\n") + 1; lines > 15 {
		t.Errorf("template is %d lines, want <= 15:\n%s", lines, stored)
	}
}

func TestFlowCreateDefaultTemplateRuns(t *testing.T) {
	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	root := NewRootCommand(r)
	root.SetOut(new(strings.Builder))
	root.SetArgs([]string{"flow", "create", "demo"})
	if err := root.Execute(); err != nil {
		t.Fatalf("flow create demo: %v", err)
	}

	engine := workflow.NewEngine(filepath.Join(r.DataDir(), "flows"), "http://localhost:9876")

	// A fresh template must run as-is and print exactly the documented line.
	out, err := engine.Run("demo", nil)
	if err != nil {
		t.Fatalf("run freshly created flow: %v", err)
	}
	if out != "Hello from flow" {
		t.Fatalf("run output = %q, want %q", out, "Hello from flow")
	}

	// The same template must also demonstrate trigger handling when a payload
	// is supplied (single-key map so the dict rendering is deterministic).
	out, err = engine.Run("demo", map[string]interface{}{"repo": "x"})
	if err != nil {
		t.Fatalf("run flow with payload: %v", err)
	}
	if out != `Triggered with payload: {"repo": "x"}` {
		t.Fatalf("payload run output = %q, want %q", out, `Triggered with payload: {"repo": "x"}`)
	}
}

func TestFlowCreateViaDashboardSendsDefaultTemplate(t *testing.T) {
	var gotSource string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/flows" {
			http.NotFound(w, req)
			return
		}
		var payload struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gotSource = payload.Source
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"demo"}`))
	}))
	defer srv.Close()

	r := app.NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	oldURL := dashboardBaseURL
	dashboardBaseURL = srv.URL
	defer func() { dashboardBaseURL = oldURL }()

	root := NewRootCommand(r)
	root.SetOut(new(strings.Builder))
	root.SetArgs([]string{"flow", "create", "demo"})
	if err := root.Execute(); err != nil {
		t.Fatalf("flow create demo (dashboard path): %v", err)
	}

	if gotSource != defaultFlowSource {
		t.Fatalf("dashboard payload source differs from defaultFlowSource\n--- got ---\n%q\n--- want ---\n%q", gotSource, defaultFlowSource)
	}
}
