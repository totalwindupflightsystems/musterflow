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

// readFixture resolves a testdata fixture through the repository tree (see
// fixturePath), never through the process working directory: a cwd-relative read
// makes the test depend on how it was invoked, which is exactly the class of
// dependence INT-CI-202 removed.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// TestConnectTolerateAcceptedSpecsAreCallable is the DF-048 criterion-2
// regression test: for every spec `connect --tolerate` accepts, `mf <api>
// --help` lists at least one command and one real operation call succeeds
// against a stub server; for every spec it cannot load, connect fails loudly
// naming the defect — it never prints "Try: musterflow <api> --help" for an API
// with an empty command list.
func TestConnectTolerateAcceptedSpecsAreCallable(t *testing.T) {
	stub := newStubAPI(t)
	// INT-CI-202: the surface asserts only what it owns — repo fixtures, temp
	// data dirs, a loopback stub — so pin the ambient inputs instead of
	// inheriting whatever the developer's shell or a sibling test left behind.
	// TestTolerateSurfaceIsHermetic re-runs this test in a pristine child
	// process and is the authoritative environment-independence proof.
	home := pinHermeticEnv(t)
	resetCLIGlobals(t)

	cases := []struct {
		name    string
		fixture string
		// apiName is the --name used at connect time.
		apiName string
		// wantPointer is the JSON pointer connect must report for a spec it
		// cannot load; empty means the spec must load and be callable.
		wantPointer string
		// call is the operation invocation that must succeed for a loaded spec.
		call []string
	}{
		{
			name:    "repairable defects load and are callable",
			fixture: "vendor-inventory.yaml",
			apiName: "vendor",
			call:    []string{"items", "list-items"},
		},
		{
			name:        "unrepairable parameters mapping is refused",
			fixture:     "vendor-inventory-blocking.yaml",
			apiName:     "blocked",
			wantPointer: "/paths/~1items~1{itemId}/get/parameters",
		},
		{
			name:        "webhooks-only document is refused",
			fixture:     "vendor-webhooks-only.yaml",
			apiName:     "hooks",
			wantPointer: "/webhooks",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataDir := t.TempDir()
			// Copy the fixture into the data-dir's sibling so the connect error
			// names a path the test can assert on.
			specDir := t.TempDir()
			specPath := filepath.Join(specDir, tc.fixture)
			if err := os.WriteFile(specPath, readFixture(t, tc.fixture), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			connect := runMF(t, dataDir, "connect", specPath,
				"--tolerate", "--name", tc.apiName, "--base-url", stub.server.URL)

			if tc.wantPointer != "" {
				if connect.err == nil {
					t.Fatalf("connect --tolerate accepted %s; it cannot load\n%s", tc.fixture, connect.combined())
				}
				msg := connect.err.Error()
				if !strings.Contains(msg, tc.fixture) {
					t.Errorf("connect error does not name the spec file:\n%s", msg)
				}
				if !strings.Contains(msg, tc.wantPointer) {
					t.Errorf("connect error does not name the blocking JSON pointer %q:\n%s", tc.wantPointer, msg)
				}
				if strings.Contains(connect.stdout, "Try: musterflow") {
					t.Errorf("connect printed a Try hint for an API it did not load:\n%s", connect.stdout)
				}
				// Nothing may be registered: a connection whose command list is
				// empty is exactly the DF-048 failure mode.
				list := runMF(t, dataDir, "list")
				if !strings.Contains(list.stdout, "No APIs connected") {
					t.Errorf("failed connect left an API registered:\n%s", list.stdout)
				}
				return
			}

			if connect.err != nil {
				t.Fatalf("connect --tolerate failed on a repairable spec: %v\n%s", connect.err, connect.combined())
			}
			// The degraded state must be reported, never silent: this is the
			// line the CI log surfaced when the surface went red, so pin it.
			if !strings.Contains(connect.combined(), "cosmetic spec defect(s)") {
				t.Errorf("connect --tolerate accepted a repairable spec without reporting the tolerated defects:\n%s", connect.combined())
			}
			if !strings.Contains(connect.stdout, "Try: musterflow "+tc.apiName+" --help") {
				t.Errorf("connect did not print the Try hint:\n%s", connect.stdout)
			}

			// `mf <api> --help` must list at least one real command.
			help := runMF(t, dataDir, tc.apiName, "--help")
			if help.err != nil {
				t.Fatalf("api help: %v\n%s", help.err, help.combined())
			}
			if !strings.Contains(help.stdout, "Available Commands:") {
				t.Fatalf("api help lists no commands:\n%s", help.combined())
			}
			if !strings.Contains(help.stdout, "items") {
				t.Errorf("api help does not list the items resource:\n%s", help.stdout)
			}

			// One real operation call must reach the network and succeed.
			before := stub.hits.Load()
			call := runMF(t, dataDir, append([]string{tc.apiName}, tc.call...)...)
			if call.err != nil {
				t.Fatalf("operation call failed: %v\n%s", call.err, call.combined())
			}
			if stub.hits.Load() == before {
				t.Fatalf("operation call never reached the API:\n%s", call.combined())
			}
			if !strings.Contains(call.stdout, "stub") {
				t.Errorf("operation call output lacks the stub response:\n%s", call.stdout)
			}
		})
	}

	// INT-CI-202: the surface must stay inside its own data dir. Anything under
	// the pinned HOME means the run resolved state outside the data dir the test
	// gave it (TestTolerateSurfaceIsHermetic proves the same thing from a fresh
	// process with a foreign cwd).
	assertEmptyDir(t, home, "pinned HOME")
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

// TestHelpOnUnloadableConnectionReportsErrorInsteadOfEmptyTree proves DF-048's
// "never print Try/empty help for an API that cannot load" rule at the help
// surface: with the plan gone and the source spec unloadable, `mf <api> --help`
// reports the load failure instead of a usage block with no commands.
func TestHelpOnUnloadableConnectionReportsErrorInsteadOfEmptyTree(t *testing.T) {
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

	// Destroy the cache, then make the source unloadable: only a loud failure
	// is acceptable from here on.
	conn := runMF(t, dataDir, "list")
	if conn.err != nil {
		t.Fatalf("list: %v", conn.err)
	}
	var connID string
	for _, line := range strings.Split(conn.stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "vendor (") {
			connID = strings.TrimSuffix(strings.TrimPrefix(line, "vendor ("), ")")
		}
	}
	if connID == "" {
		t.Fatalf("could not read the connection id from list output:\n%s", conn.stdout)
	}
	if err := app.RemovePlan(dataDir, connID); err != nil {
		t.Fatalf("remove plan: %v", err)
	}
	if err := os.WriteFile(specPath, readFixture(t, "vendor-webhooks-only.yaml"), 0o644); err != nil {
		t.Fatalf("rewrite spec: %v", err)
	}

	help := runMF(t, dataDir, "vendor", "--help")
	combined := help.combined()
	if !strings.Contains(combined, "cannot be loaded") {
		t.Fatalf("help did not report the load failure:\n%s", combined)
	}
	if strings.Contains(help.stdout, "Available Commands:") {
		t.Fatalf("help printed a command listing for an API that cannot load:\n%s", help.stdout)
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
