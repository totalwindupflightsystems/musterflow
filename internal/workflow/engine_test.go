package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── AC-020.1: NewEngine constructor ──────────────────────────────────────

func TestNewEngine(t *testing.T) {
	e := NewEngine("/tmp/flows", "http://localhost:9876")
	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.dir != "/tmp/flows" {
		t.Errorf("dir = %q, want /tmp/flows", e.dir)
	}
	if e.baseURL != "http://localhost:9876" {
		t.Errorf("baseURL = %q, want http://localhost:9876", e.baseURL)
	}
}

// ── AC-020.2: Create tests ──────────────────────────────────────────────

func TestCreate_WithoutWebhook(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	flow, err := e.Create("hello", "print('hello')", "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if flow.Name != "hello" {
		t.Errorf("Name = %q, want hello", flow.Name)
	}
	if flow.Source != "print('hello')" {
		t.Errorf("Source = %q", flow.Source)
	}
	if flow.Webhook {
		t.Error("Webhook should be false")
	}
	if flow.WebhookURL != "" {
		t.Errorf("WebhookURL = %q, want empty", flow.WebhookURL)
	}
	if flow.Description != "" {
		t.Errorf("Description = %q, want empty", flow.Description)
	}

	// Verify file written to disk
	data, err := os.ReadFile(filepath.Join(dir, "hello.star"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "print('hello')" {
		t.Errorf("file content = %q, want print('hello')", string(data))
	}
}

func TestCreate_WithWebhook(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	flow, err := e.Create("webhook-flow", "print('triggered')", "", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !flow.Webhook {
		t.Error("Webhook should be true")
	}
	if flow.WebhookURL != "http://localhost:9876/hooks/webhook-flow" {
		t.Errorf("WebhookURL = %q, want http://localhost:9876/hooks/webhook-flow", flow.WebhookURL)
	}
}

func TestCreate_DuplicateOverwrites(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("dup", "v1", "", false)
	if err != nil {
		t.Fatalf("Create v1: %v", err)
	}

	flow, err := e.Create("dup", "v2", "updated", true)
	if err != nil {
		t.Fatalf("Create v2: %v", err)
	}

	if flow.Source != "v2" {
		t.Errorf("Source = %q, want v2", flow.Source)
	}
	if !flow.Webhook {
		t.Error("Webhook should be true after overwrite")
	}
	if flow.Description != "updated" {
		t.Errorf("Description = %q, want updated", flow.Description)
	}

	// File should contain v2
	data, _ := os.ReadFile(filepath.Join(dir, "dup.star"))
	if string(data) != "v2" {
		t.Errorf("file content = %q, want v2", string(data))
	}
}

func TestCreate_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "nested")
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("nested-flow", "pass", "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Verify nested directory was created
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("dir should be a directory")
	}
}

// TestCreate_PersistsMeta verifies the sidecar metadata file is written and
// can be read back via readMeta.
func TestCreate_PersistsMeta(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("meta-flow", "print('x')", "my description", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	meta := e.readMeta("meta-flow")
	if meta.Description != "my description" {
		t.Errorf("meta.Description = %q, want %q", meta.Description, "my description")
	}
	if !meta.Webhook {
		t.Error("meta.Webhook should be true")
	}
}

// ── AC-020.3: List tests ─────────────────────────────────────────────────

func TestList_Empty(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 0 {
		t.Errorf("len(flows) = %d, want 0", len(flows))
	}
}

func TestList_SingleFlow(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("single", "source1", "", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("len(flows) = %d, want 1", len(flows))
	}
	if flows[0].Name != "single" {
		t.Errorf("Name = %q, want single", flows[0].Name)
	}
	if flows[0].Source != "source1" {
		t.Errorf("Source = %q", flows[0].Source)
	}
	if !flows[0].Webhook {
		t.Error("Webhook should be true from sidecar")
	}
	if flows[0].WebhookURL != "http://localhost:9876/hooks/single" {
		t.Errorf("WebhookURL = %q", flows[0].WebhookURL)
	}
}

func TestList_MultipleFlows(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, _ = e.Create("a", "source-a", "", false)
	_, _ = e.Create("b", "source-b", "", true)
	_, _ = e.Create("c", "source-c", "", false)

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 3 {
		t.Fatalf("len(flows) = %d, want 3", len(flows))
	}

	// Verify all names present (order may vary — os.ReadDir)
	names := make(map[string]bool)
	for _, f := range flows {
		names[f.Name] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !names[want] {
			t.Errorf("missing flow %q", want)
		}
	}
}

func TestList_SkipsNonStarFiles(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, _ = e.Create("flow1", "real", "", false)
	// Write a non-.star file
	_ = os.WriteFile(filepath.Join(dir, "README.txt"), []byte("docs"), 0644)
	// Write a directory
	_ = os.MkdirAll(filepath.Join(dir, "subdir"), 0755)

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("len(flows) = %d, want 1 (only .star files)", len(flows))
	}
	if flows[0].Name != "flow1" {
		t.Errorf("Name = %q, want flow1", flows[0].Name)
	}
}

func TestList_CreatesDirectoryOnEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	e := NewEngine(dir, "http://localhost:9876")

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 0 {
		t.Errorf("len(flows) = %d, want 0", len(flows))
	}

	// Verify directory was created
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("dir should exist")
	}
}

// ── AC-020.4: Run tests ──────────────────────────────────────────────────

func TestRun_ComputedOutput(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("compute", "print(6 * 7)", "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("compute", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != "42" {
		t.Errorf("output = %q, want 42", output)
	}
}

func TestRun_NonexistentFlow(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Run("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent flow")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error = %v, want mention of nonexistent", err)
	}
}

func TestRun_WithTriggerPayload(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	// Flow references the predeclared "trigger" global.
	_, err := e.Create("trigger-flow", `print(trigger["user"])`, "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("trigger-flow", map[string]interface{}{
		"user": "alice",
		"id":   42,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != "alice" {
		t.Errorf("output = %q, want alice", output)
	}
}

func TestRun_NilPayload(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	// Flow without trigger references still runs with nil payload.
	_, err := e.Create("no-trigger", "print('ok')", "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("no-trigger", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != "ok" {
		t.Errorf("output = %q, want ok", output)
	}
}

func TestRun_NilPayload_TriggerIsNone(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	// Flow that uses a conditional expression to check trigger is None.
	_, err := e.Create("trigger-none", `print("none" if trigger == None else "set")`, "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("trigger-none", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != "none" {
		t.Errorf("output = %q, want none", output)
	}
}

func TestRun_SyntaxError(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("bad-syntax", `print(`, "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = e.Run("bad-syntax", nil)
	if err == nil {
		t.Fatal("expected error for syntax-error flow")
	}
	if !strings.Contains(err.Error(), "parse flow bad-syntax") {
		t.Errorf("error = %v, want parse error mentioning flow name", err)
	}
}

// ── DF-030: HTTP builtins ────────────────────────────────────────────────

// TestRun_HTTPGet proves http_get returns the status/body/headers dict and that
// a flow can print fields of it.
func TestRun_HTTPGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Reply", "hello")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	src := `resp = http_get("` + srv.URL + `")
print(resp["status"])
print(resp["body"])
print(resp["headers"]["X-Reply"])`
	if _, err := e.Create("get-flow", src, "", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("get-flow", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "200\n{\"ok\":true}\nhello"
	if output != want {
		t.Errorf("output = %q, want %q", output, want)
	}
}

// TestRun_HTTPPost proves http_post sends the Starlark body and headers dict and
// that a custom request header reaches the server.
func TestRun_HTTPPost(t *testing.T) {
	type seen struct {
		method string
		body   string
		header string
	}
	got := make(chan seen, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- seen{method: r.Method, body: string(body), header: r.Header.Get("X-Flow")}
		_ = json.NewEncoder(w).Encode(map[string]string{"method": r.Method})
	}))
	defer srv.Close()

	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	src := `resp = http_post("` + srv.URL + `", body="hello", headers={"X-Flow": "yes"})
print(resp["body"])`
	if _, err := e.Create("post-flow", src, "", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("post-flow", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(output, `"method":"POST"`) {
		t.Errorf("output = %q, want echoed method POST", output)
	}

	select {
	case s := <-got:
		if s.method != http.MethodPost {
			t.Errorf("server saw method = %q, want POST", s.method)
		}
		if s.body != "hello" {
			t.Errorf("server saw body = %q, want hello", s.body)
		}
		if s.header != "yes" {
			t.Errorf("server saw X-Flow = %q, want yes", s.header)
		}
	default:
		t.Fatal("server never received the request")
	}
}

// TestRun_HTTPNon2xxIsNotAnError proves an HTTP error status is returned as
// data (a dict) instead of raising, so a flow can branch on it.
func TestRun_HTTPNon2xxIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone fishing", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	src := `resp = http_get("` + srv.URL + `")
print("not found" if resp["status"] == 404 else "unexpected")`
	if _, err := e.Create("404-flow", src, "", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("404-flow", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != "not found" {
		t.Errorf("output = %q, want \"not found\"", output)
	}
}

// TestRun_HTTPTransportError proves a connection failure surfaces as a Starlark
// error naming the failing builtin.
func TestRun_HTTPTransportError(t *testing.T) {
	// Grab an ephemeral port and close it again so nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	src := `http_get("http://` + addr + `/nope")`
	if _, err := e.Create("dead-flow", src, "", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = e.Run("dead-flow", nil)
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if !strings.Contains(err.Error(), "http_get") {
		t.Errorf("error = %v, want it to name http_get", err)
	}
}

// TestRun_UnknownGlobalStillFails proves installing builtins did not open the
// compiler to arbitrary globals.
func TestRun_UnknownGlobalStillFails(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	if _, err := e.Create("bogus-flow", `bogus_fn()`, "", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err := e.Run("bogus-flow", nil)
	if err == nil {
		t.Fatal("expected an undefined-global error")
	}
	if !strings.Contains(err.Error(), "undefined: bogus_fn") {
		t.Errorf("error = %v, want it to mention undefined: bogus_fn", err)
	}
}

// TestRun_TriggerAndBuiltinsCoexist proves the trigger payload and the HTTP
// builtins are visible in the same flow.
func TestRun_TriggerAndBuiltinsCoexist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/echo/alice" {
			t.Errorf("path = %q, want /echo/alice", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	src := `resp = http_get("` + srv.URL + `/echo/" + trigger["user"])
print(trigger["user"] + ":" + resp["body"])`
	if _, err := e.Create("combo-flow", src, "", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("combo-flow", map[string]interface{}{"user": "alice"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != "alice:pong" {
		t.Errorf("output = %q, want alice:pong", output)
	}
}

// TestRun_HTTPBuiltinArgumentErrors covers the closed-set and type validation
// the builtins perform before any request is made.
func TestRun_HTTPBuiltinArgumentErrors(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	tests := []struct {
		name    string
		source  string
		wantSub string
	}{
		{
			name:    "invalid method lists valid methods",
			source:  `http_request("TRACE", "http://127.0.0.1:1/")`,
			wantSub: "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS",
		},
		{
			name:    "non-dict headers rejected",
			source:  `http_get("http://127.0.0.1:1/", headers=["a"])`,
			wantSub: "headers must be a dict of str to str",
		},
		{
			name:    "non-str header value rejected",
			source:  `http_get("http://127.0.0.1:1/", headers={"X": 1})`,
			wantSub: "headers values must be strings",
		},
		{
			name:    "GET with body rejected",
			source:  `http_request("GET", "http://127.0.0.1:1/", body="x")`,
			wantSub: "GET requests must not include a body",
		},
		{
			name:    "non-string body rejected",
			source:  `http_post("http://127.0.0.1:1/", body=42)`,
			wantSub: "body must be a string or bytes",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			flowName := strings.ReplaceAll(tc.name, " ", "-")
			if _, err := e.Create(flowName, tc.source, "", false); err != nil {
				t.Fatalf("Create: %v", err)
			}
			_, err := e.Run(flowName, nil)
			if err == nil {
				t.Fatalf("expected an error for %q", tc.source)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantSub)
			}
		})
	}
}

// TestRun_HTTPResponseBodyCapped proves oversized responses are truncated to
// 1 MiB instead of buffered in full.
func TestRun_HTTPResponseBodyCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), flowMaxResponseBody+4096))
	}))
	defer srv.Close()

	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	src := `resp = http_get("` + srv.URL + `")
print(len(resp["body"]))`
	if _, err := e.Create("big-flow", src, "", false); err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("big-flow", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if output != fmt.Sprintf("%d", flowMaxResponseBody) {
		t.Errorf("body length = %q, want %d", output, flowMaxResponseBody)
	}
}

// --- PERF-046: Benchmarks for hot paths ---

func BenchmarkEngine_Run(b *testing.B) {
	e := NewEngine(b.TempDir(), "http://localhost:9876")
	_, _ = e.Create("bench-flow", "pass", "", false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = e.Run("bench-flow", nil)
	}
}

// ── E2E38 regression: description persistence + webhook URL correctness ──

// TestList_DescriptionRoundTrip verifies a description set at create time
// survives a create → list round-trip via the sidecar metadata file.
func TestList_DescriptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("desc-flow", "print('x')", "my desc", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("len(flows) = %d, want 1", len(flows))
	}
	if flows[0].Description != "my desc" {
		t.Errorf("Description = %q, want %q", flows[0].Description, "my desc")
	}
}

// TestList_NoWebhookURLForNonWebhookFlow verifies List() does NOT synthesize a
// webhook URL for flows created without --webhook (E2E38-003).
func TestList_NoWebhookURLForNonWebhookFlow(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("plain", "print('x')", "", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("len(flows) = %d, want 1", len(flows))
	}
	if flows[0].Webhook {
		t.Error("Webhook should be false")
	}
	if flows[0].WebhookURL != "" {
		t.Errorf("WebhookURL = %q, want empty for non-webhook flow", flows[0].WebhookURL)
	}
}

// TestList_WebhookURLForWebhookFlow verifies List() still synthesizes a
// webhook URL for flows created with --webhook (regression).
func TestList_WebhookURLForWebhookFlow(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("hook", "print('x')", "", true)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("len(flows) = %d, want 1", len(flows))
	}
	if !flows[0].Webhook {
		t.Error("Webhook should be true")
	}
	if flows[0].WebhookURL != "http://localhost:9876/hooks/hook" {
		t.Errorf("WebhookURL = %q, want http://localhost:9876/hooks/hook", flows[0].WebhookURL)
	}
}

// TestList_LegacyFlowWithoutSidecar verifies a .star file without a sidecar
// (created before sidecars existed) is still listed, defaults to
// Webhook=false, and does not advertise a webhook URL.
func TestList_LegacyFlowWithoutSidecar(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	// Write a bare .star file with no sidecar (legacy).
	_ = os.WriteFile(filepath.Join(dir, "legacy.star"), []byte("print('old')"), 0644)

	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("len(flows) = %d, want 1", len(flows))
	}
	if flows[0].Name != "legacy" {
		t.Errorf("Name = %q, want legacy", flows[0].Name)
	}
	if flows[0].Webhook {
		t.Error("Webhook should be false for legacy flow")
	}
	if flows[0].WebhookURL != "" {
		t.Errorf("WebhookURL = %q, want empty for legacy flow", flows[0].WebhookURL)
	}
	if flows[0].Description != "" {
		t.Errorf("Description = %q, want empty for legacy flow", flows[0].Description)
	}
}

// TestList_SkipsSidecarFiles verifies .star.json sidecar files are NOT listed
// as flows (they have extension .json, not .star, so they're already skipped,
// but this test guards against future regressions).
func TestList_SkipsSidecarFiles(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, _ = e.Create("real", "print('x')", "desc", true)
	flows, err := e.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("len(flows) = %d, want 1 (sidecar must not be listed)", len(flows))
	}
	if flows[0].Name != "real" {
		t.Errorf("Name = %q, want real", flows[0].Name)
	}
}
