package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/totalwindupflightsystems/musterflow/internal/app"
)

// --- DF-048 / PERF-002 regression helpers --------------------------------

// resetAPICommandCache clears the per-process command cache so a test can act
// like a fresh CLI process. The real cache lives for one process lifetime.
func resetAPICommandCache() {
	apiCommandsMu.Lock()
	apiCommands = make(map[string]*apiCommandState)
	apiCommandsMu.Unlock()
}

// mfResult is one in-process CLI invocation.
type mfResult struct {
	stdout string
	stderr string
	err    error
}

// combined is what a user sees: stdout+stderr, plus the error the CLI exits on.
func (r mfResult) combined() string { return r.stdout + r.stderr }

// runMF executes one CLI invocation against dataDir, from a fresh command tree
// (so it behaves like a separate process). Output is captured at the process
// level because the CLI prints through fmt (os.Stdout/os.Stderr), exactly as a
// user would see it.
func runMF(t *testing.T, dataDir string, args ...string) mfResult {
	t.Helper()
	resetAPICommandCache()

	reg := app.NewRegistry(dataDir)
	if err := reg.Load(); err != nil {
		t.Fatalf("registry load: %v", err)
	}
	defer func() { _ = reg.Close() }()

	root := NewRootCommand(reg)
	root.SetArgs(args)

	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	os.Stdout, os.Stderr = wOut, wErr

	var outBuf, errBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(&outBuf, rOut) }()
	go func() { defer wg.Done(); _, _ = io.Copy(&errBuf, rErr) }()

	runErr := root.Execute()

	os.Stdout, os.Stderr = oldOut, oldErr
	_ = wOut.Close()
	_ = wErr.Close()
	wg.Wait()
	_ = rOut.Close()
	_ = rErr.Close()

	return mfResult{stdout: outBuf.String(), stderr: errBuf.String(), err: runErr}
}

// stubAPI serves every request with a JSON body and counts hits.
type stubAPI struct {
	server *httptest.Server
	hits   atomic.Int64
}

func newStubAPI(t *testing.T) *stubAPI {
	t.Helper()
	stub := &stubAPI{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"stub": true, "items": []}`)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

// specServer serves a spec body at one path and counts how often it is fetched.
type specServer struct {
	server *httptest.Server
	hits   atomic.Int64
	mu     sync.Mutex
	body   []byte
}

func newSpecServer(t *testing.T, body []byte) *specServer {
	t.Helper()
	s := &specServer{body: body}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi.yaml" {
			http.NotFound(w, r)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.hits.Add(1)
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(s.body)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *specServer) specURL() string { return s.server.URL + "/openapi.yaml" }

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestOperationCallNeverReFetchesSpec is the PERF-002 criterion-2 regression
// test: once an API is connected by URL, operation calls are served from the
// cached plan and add ZERO GETs to the spec URL.
func TestOperationCallNeverReFetchesSpec(t *testing.T) {
	stub := newStubAPI(t)
	specs := newSpecServer(t, readFixture(t, "vendor-inventory.yaml"))
	dataDir := t.TempDir()

	connect := runMF(t, dataDir, "connect", specs.specURL(),
		"--tolerate", "--name", "vendor", "--base-url", stub.server.URL)
	if connect.err != nil {
		t.Fatalf("connect: %v\n%s", connect.err, connect.combined())
	}
	if got := specs.hits.Load(); got != 1 {
		t.Fatalf("connect fetched the spec %d times, want 1", got)
	}

	before := specs.hits.Load()
	for i := 0; i < 3; i++ {
		call := runMF(t, dataDir, "vendor", "items", "list-items")
		if call.err != nil {
			t.Fatalf("operation call %d failed: %v\n%s", i+1, call.err, call.combined())
		}
	}
	if got := specs.hits.Load() - before; got != 0 {
		t.Fatalf("3 operation calls added %d GET(s) to the spec URL, want 0", got)
	}
}

// TestOperationCallsSurviveSourceSpecRemoval proves the store does not depend on
// the source spec at call time (PERF-002 criterion 4).
func TestOperationCallsSurviveSourceSpecRemoval(t *testing.T) {
	stub := newStubAPI(t)
	specDir := t.TempDir()
	specPath := filepath.Join(specDir, "vendor.yaml")
	if err := os.WriteFile(specPath, readFixture(t, "vendor-inventory.yaml"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	dataDir := t.TempDir()

	connect := runMF(t, dataDir, "connect", specPath,
		"--tolerate", "--name", "vendor", "--base-url", stub.server.URL)
	if connect.err != nil {
		t.Fatalf("connect: %v\n%s", connect.err, connect.combined())
	}

	if err := os.Rename(specPath, specPath+".moved"); err != nil {
		t.Fatalf("move source spec: %v", err)
	}

	for i := 0; i < 3; i++ {
		call := runMF(t, dataDir, "vendor", "items", "list-items")
		if call.err != nil {
			t.Fatalf("operation call %d after source removal failed: %v\n%s", i+1, call.err, call.combined())
		}
		if !strings.Contains(call.stdout, "stub") {
			t.Errorf("operation call %d output lacks the stub response:\n%s", i+1, call.stdout)
		}
	}
}

// TestResourceGroupsAreLazyButComplete proves the lazy per-resource loading keeps
// the tree complete: `--help` lists every resource group, an operation in the
// invoked resource works, and an operation in a resource that was NOT the
// invoked one works too (loaded on demand).
func TestResourceGroupsAreLazyButComplete(t *testing.T) {
	stub := newStubAPI(t)
	specDir := t.TempDir()
	specPath := filepath.Join(specDir, "vendor.yaml")
	if err := os.WriteFile(specPath, readFixture(t, "vendor-inventory.yaml"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	dataDir := t.TempDir()

	connect := runMF(t, dataDir, "connect", specPath,
		"--tolerate", "--name", "vendor", "--base-url", stub.server.URL)
	if connect.err != nil {
		t.Fatalf("connect: %v\n%s", connect.err, connect.combined())
	}

	// The full help listing must show both resource groups.
	help := runMF(t, dataDir, "vendor", "--help")
	for _, want := range []string{"items", "vendors"} {
		if !strings.Contains(help.stdout, want) {
			t.Errorf("api help does not list resource %q:\n%s", want, help.stdout)
		}
	}

	// Invoking one resource must not hide another: each call is its own process.
	items := runMF(t, dataDir, "vendor", "items", "list-items")
	if items.err != nil {
		t.Fatalf("items call: %v\n%s", items.err, items.combined())
	}
	vendors := runMF(t, dataDir, "vendor", "vendors", "list-vendors")
	if vendors.err != nil {
		t.Fatalf("vendors call: %v\n%s", vendors.err, vendors.combined())
	}

	// Group help (a lazy group's own --help) lists its operations.
	groupHelp := runMF(t, dataDir, "vendor", "vendors", "--help")
	if !strings.Contains(groupHelp.stdout, "list-vendors") {
		t.Errorf("vendors --help does not list list-vendors:\n%s", groupHelp.combined())
	}

	// An unknown resource/operation still fails loudly.
	unknown := runMF(t, dataDir, "vendor", "nope", "list")
	if unknown.err == nil {
		t.Fatalf("unknown resource succeeded:\n%s", unknown.combined())
	}
}

// TestRefreshRewritesCommandPlan proves a refresh picks up new operations from
// the source spec and replaces the cached command plan.
func TestRefreshRewritesCommandPlan(t *testing.T) {
	stub := newStubAPI(t)
	specDir := t.TempDir()
	specPath := filepath.Join(specDir, "vendor.yaml")
	if err := os.WriteFile(specPath, readFixture(t, "vendor-inventory.yaml"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	dataDir := t.TempDir()

	connect := runMF(t, dataDir, "connect", specPath,
		"--tolerate", "--name", "vendor", "--base-url", stub.server.URL)
	if connect.err != nil {
		t.Fatalf("connect: %v\n%s", connect.err, connect.combined())
	}
	if out := runMF(t, dataDir, "vendor", "items", "--help"); !strings.Contains(out.stdout, "list-items") {
		t.Fatalf("items help missing list-items before refresh:\n%s", out.combined())
	}

	refreshed := strings.Replace(string(readFixture(t, "vendor-inventory.yaml")), "\ncomponents:", `
  /reports:
    get:
      operationId: list-reports
      summary: List reports
      responses:
        "200":
          description: ok
components:`, 1)
	if err := os.WriteFile(specPath, []byte(refreshed), 0o644); err != nil {
		t.Fatalf("rewrite spec: %v", err)
	}

	if out := runMF(t, dataDir, "refresh", "vendor"); out.err != nil {
		t.Fatalf("refresh: %v\n%s", out.err, out.combined())
	}

	if out := runMF(t, dataDir, "vendor", "reports", "--help"); out.err != nil {
		t.Fatalf("reports group after refresh: %v\n%s", out.err, out.combined())
	} else if !strings.Contains(out.stdout, "list-reports") {
		t.Fatalf("refreshed plan does not include the new operation:\n%s", out.combined())
	}
}
