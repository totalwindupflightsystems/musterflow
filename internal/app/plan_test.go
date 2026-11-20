package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// twoResourceSpec is a small, valid 3.1.0 document with operations in two
// resource groups, used to exercise the plan cache's per-resource layout.
const twoResourceSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "Two Resources", "version": "1.0.0"},
  "servers": [{"url": "http://example.test/v1"}],
  "paths": {
    "/items": {
      "get": {"operationId": "list-items", "summary": "List items", "responses": {"200": {"description": "ok"}}},
      "post": {"operationId": "create-item", "summary": "Create item", "responses": {"201": {"description": "ok"}}}
    },
    "/widgets": {
      "get": {"operationId": "list-widgets", "summary": "List widgets", "responses": {"200": {"description": "ok"}}}
    }
  }
}`

func writeTempSpec(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry(t.TempDir())
	if err := r.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// TestConnectCachesPerResourcePlan proves connect persists a plan whose index
// splits operations by the resource groups the generator itself produced, and
// that the per-resource read returns exactly one group's operations.
func TestConnectCachesPerResourcePlan(t *testing.T) {
	r := newTestRegistry(t)
	specPath := writeTempSpec(t, t.TempDir(), "two.json", twoResourceSpec)

	res, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	idx, err := ReadPlanIndex(r.DataDir(), res.Connection)
	if err != nil {
		t.Fatalf("ReadPlanIndex: %v", err)
	}
	names := map[string]bool{}
	for _, res := range idx.Resources() {
		names[res.Name] = true
	}
	if !names["items"] || !names["widgets"] {
		t.Fatalf("plan index resources = %v, want items and widgets", names)
	}

	itemsOps, err := idx.ResourceOperations(r.DataDir(), res.Connection, "items")
	if err != nil {
		t.Fatalf("ResourceOperations(items): %v", err)
	}
	if len(itemsOps) != 2 {
		t.Fatalf("items resource has %d operations, want 2", len(itemsOps))
	}
	for _, op := range itemsOps {
		if !strings.HasPrefix(op.Path, "/items") {
			t.Errorf("items resource contains operation from %q", op.Path)
		}
	}

	widgetsOps, err := idx.ResourceOperations(r.DataDir(), res.Connection, "widgets")
	if err != nil {
		t.Fatalf("ResourceOperations(widgets): %v", err)
	}
	if len(widgetsOps) != 1 {
		t.Fatalf("widgets resource has %d operations, want 1", len(widgetsOps))
	}

	all, err := idx.AllOperations(r.DataDir(), res.Connection)
	if err != nil {
		t.Fatalf("AllOperations: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("AllOperations returned %d operations, want 3", len(all))
	}

	// Unknown resources are a cache miss, never an empty-but-successful read.
	if _, err := idx.ResourceOperations(r.DataDir(), res.Connection, "nope"); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("ResourceOperations(unknown) error = %v, want ErrNoPlan", err)
	}
}

// TestPlanCacheSurvivesSourceRemoval proves a connected API keeps working after
// its source spec file is gone: every operation call is served from the cache.
func TestPlanCacheSurvivesSourceRemoval(t *testing.T) {
	r := newTestRegistry(t)
	dir := t.TempDir()
	specPath := writeTempSpec(t, dir, "two.json", twoResourceSpec)

	res, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := os.Remove(specPath); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	idx, err := ReadPlanIndex(r.DataDir(), res.Connection)
	if err != nil {
		t.Fatalf("ReadPlanIndex after source removal: %v", err)
	}
	ops, err := idx.ResourceOperations(r.DataDir(), res.Connection, "items")
	if err != nil {
		t.Fatalf("ResourceOperations after source removal: %v", err)
	}
	commands, err := CommandsFromPlan(idx.Plan(ops), res.Connection)
	if err != nil {
		t.Fatalf("CommandsFromPlan: %v", err)
	}
	if len(commands) != 1 || commands[0].Name() != "items" {
		t.Fatalf("commands = %v, want one items group", commandNames(commands))
	}
	if len(commands[0].Commands()) != 2 {
		t.Fatalf("items group has %d operations, want 2", len(commands[0].Commands()))
	}
}

// TestReconnectReplacesCachedPlan proves a re-connect invalidates the cache: the
// plan's content hash changes and resources that no longer exist disappear.
func TestReconnectReplacesCachedPlan(t *testing.T) {
	r := newTestRegistry(t)
	dir := t.TempDir()
	specPath := writeTempSpec(t, dir, "two.json", twoResourceSpec)

	first, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	idxBefore, err := ReadPlanIndex(r.DataDir(), first.Connection)
	if err != nil {
		t.Fatalf("ReadPlanIndex: %v", err)
	}

	// Same source path, different content: only the widgets resource remains.
	writeTempSpec(t, dir, "two.json", `{
	  "openapi": "3.1.0",
	  "info": {"title": "Two Resources", "version": "1.1.0"},
	  "servers": [{"url": "http://example.test/v1"}],
	  "paths": {
	    "/widgets": {
	      "get": {"operationId": "list-widgets", "summary": "List widgets", "responses": {"200": {"description": "ok"}}},
	      "delete": {"operationId": "delete-widget", "summary": "Delete widget", "responses": {"204": {"description": "gone"}}}
	    }
	  }
	}`)

	second, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath})
	if err != nil {
		t.Fatalf("re-Connect: %v", err)
	}
	idxAfter, err := ReadPlanIndex(r.DataDir(), second.Connection)
	if err != nil {
		t.Fatalf("ReadPlanIndex after reconnect: %v", err)
	}

	if idxBefore.doc.ContentSHA256 == "" {
		t.Fatal("first plan has no content hash")
	}
	if idxBefore.doc.ContentSHA256 == idxAfter.doc.ContentSHA256 {
		t.Fatal("reconnect did not change the plan content hash")
	}
	if idxAfter.HasResource("items") {
		t.Fatal("stale items resource survived a reconnect")
	}
	if !idxAfter.HasResource("widgets") {
		t.Fatal("widgets resource missing after reconnect")
	}
	ops, err := idxAfter.ResourceOperations(r.DataDir(), second.Connection, "widgets")
	if err != nil {
		t.Fatalf("ResourceOperations(widgets): %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("widgets has %d operations after reconnect, want 2", len(ops))
	}
}

// TestDisconnectRemovesCachedPlan proves disconnecting drops the cache, so a
// later connect starts from the source instead of a stale plan.
func TestDisconnectRemovesCachedPlan(t *testing.T) {
	r := newTestRegistry(t)
	specPath := writeTempSpec(t, t.TempDir(), "two.json", twoResourceSpec)

	res, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := ReadPlanIndex(r.DataDir(), res.Connection); err != nil {
		t.Fatalf("plan missing after connect: %v", err)
	}

	if err := Disconnect(r, res.Connection.ID); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if _, err := ReadPlanIndex(r.DataDir(), res.Connection); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("plan survived disconnect: err = %v", err)
	}
	if _, err := os.Stat(PlanDir(r.DataDir(), res.Connection.ID)); !os.IsNotExist(err) {
		t.Fatalf("plan directory survived disconnect: %v", err)
	}
}

// TestReadPlanIndexMissesOnForeignConnection proves a plan is only served to the
// connection it was written for (a stale/reused id or a changed source URL is a
// cache miss, never silently-wrong commands).
func TestReadPlanIndexMissesOnForeignConnection(t *testing.T) {
	r := newTestRegistry(t)
	specPath := writeTempSpec(t, t.TempDir(), "two.json", twoResourceSpec)

	res, err := Connect(context.Background(), r, ConnectOptions{SpecURL: specPath})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	foreign := *res.Connection
	foreign.SpecURL = specPath + ".moved"
	if _, err := ReadPlanIndex(r.DataDir(), &foreign); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("plan served for a different source URL: err = %v", err)
	}
}

func commandNames(commands []*cobra.Command) []string {
	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.Name())
	}
	return names
}
