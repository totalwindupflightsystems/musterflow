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

	flow, err := e.Create("hello", "print('hello')", false)
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

	flow, err := e.Create("webhook-flow", "print('triggered')", true)
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

	_, err := e.Create("dup", "v1", false)
	if err != nil {
		t.Fatalf("Create v1: %v", err)
	}

	flow, err := e.Create("dup", "v2", true)
	if err != nil {
		t.Fatalf("Create v2: %v", err)
	}

	if flow.Source != "v2" {
		t.Errorf("Source = %q, want v2", flow.Source)
	}
	if !flow.Webhook {
		t.Error("Webhook should be true after overwrite")
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

	_, err := e.Create("nested-flow", "pass", false)
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

	_, err := e.Create("single", "source1", true)
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
	if flows[0].WebhookURL != "http://localhost:9876/hooks/single" {
		t.Errorf("WebhookURL = %q", flows[0].WebhookURL)
	}
}

func TestList_MultipleFlows(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, _ = e.Create("a", "source-a", false)
	_, _ = e.Create("b", "source-b", true)
	_, _ = e.Create("c", "source-c", false)

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

	_, _ = e.Create("flow1", "real", false)
	// Write a non-.star file
	os.WriteFile(filepath.Join(dir, "README.txt"), []byte("docs"), 0644)
	// Write a directory
	os.MkdirAll(filepath.Join(dir, "subdir"), 0755)

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

func TestRun_ExistingFlow(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("test-flow", "print('run')", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("test-flow", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(output, `"test-flow" executed successfully`) {
		t.Errorf("output = %q, want success message", output)
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

func TestRun_WithPayload(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("payload-flow", "print('trigger')", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("payload-flow", map[string]interface{}{
		"user": "alice",
		"id":   42,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(output, "alice") {
		t.Errorf("output = %q, want mention of trigger payload", output)
	}
	if !strings.Contains(output, "42") {
		t.Errorf("output = %q, want mention of trigger payload id", output)
	}
}

func TestRun_NilPayload(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir, "http://localhost:9876")

	_, err := e.Create("nil-payload", "pass", false)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	output, err := e.Run("nil-payload", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(output, "executed successfully") {
		t.Errorf("output = %q, want success message", output)
	}
	// nil payload should not include "Trigger payload:" in output
	if strings.Contains(output, "Trigger payload:") {
		t.Error("nil payload should not include trigger payload in output")
	}
}
