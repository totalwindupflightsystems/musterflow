package workflow

import (
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
