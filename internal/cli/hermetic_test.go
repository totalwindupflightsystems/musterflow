package cli

// INT-CI-202 — hermeticity harness for the CLI surface tests.
//
// The tolerated-spec surface asserts product behaviour that is only meaningful
// when the test owns every one of its inputs: fixtures inside the repository, a
// data dir under t.TempDir(), no ambient env or cwd-relative resolution, no
// process-global leakage from a sibling test, and no outbound HTTP beyond the
// loopback stub. The helpers below make those inputs explicit, and
// TestTolerateSurfaceIsHermetic FAILS when one of them stops holding.
//
// The authoritative proof is a re-exec: TestTolerateSurfaceIsHermetic runs the
// tolerated-spec test again in a CHILD process whose entire environment is
// constructed by the parent (fresh HOME/XDG, fresh cwd, every outbound request
// proxied to a sink that records and refuses). That is the only way to prove
// environment independence — http.ProxyFromEnvironment caches the process
// environment on first use, so swapping env vars or transports in-process
// cannot demonstrate anything about the environment the binary started with.
//
// What this harness deliberately does NOT cover: the Go module graph. The
// engine (github.com/wojons/muster) is resolved from go.mod/go.sum in CI, but a
// developer's generated (gitignored) go.work can replace it with a local
// checkout. That is a BUILD input, not a runtime one — it is pinned and proven
// by running the suite with GOWORK=off against a pristine GOCACHE/GOMODCACHE,
// and go.mod's pin is guarded by the "engine dependency is pinned" subtest.

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// tolerateFixtures are the specs the `connect --tolerate` surface asserts on.
// They must live in this package's testdata and be self-contained: a remote
// $ref would make connect depend on the network.
var tolerateFixtures = []string{
	"vendor-inventory.yaml",
	"vendor-inventory-blocking.yaml",
	"vendor-webhooks-only.yaml",
}

// packageDir is this package's source directory, derived from the test binary's
// own compile-time path — never from the process working directory, which is
// exactly the ambient input this harness exists to eliminate.
func packageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed: cannot locate the package directory")
	}
	dir := filepath.Dir(file)
	if !filepath.IsAbs(dir) {
		t.Fatalf("package dir %q is not absolute; fixtures cannot be anchored to the repo", dir)
	}
	return dir
}

// repoRoot walks up from the package directory to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir := packageDir(t)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from %s", packageDir(t))
		}
		dir = parent
	}
}

// fixturePath resolves a testdata fixture inside the repository and fails when
// it is missing, empty, or resolves outside the repo tree.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	root := repoRoot(t)
	path := filepath.Join(packageDir(t), "testdata", name)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("fixture %s is not a regular file (%s)", name, info.Mode())
	}
	if info.Size() == 0 {
		t.Fatalf("fixture %s is empty", name)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Fatalf("fixture %s resolves outside the repository (%s, repo root %s)", name, path, root)
	}
	return path
}

// pinHermeticEnv points every ambient path variable at a fresh temp dir and
// blanks the CLI's own overrides, so the surface under test cannot read the
// developer's or the runner's state. It returns the pinned HOME so a test can
// assert the CLI wrote nothing there.
func pinHermeticEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	pinned := map[string]string{
		"HOME":                        home,
		"XDG_CONFIG_HOME":             filepath.Join(home, ".config"),
		"XDG_CACHE_HOME":              filepath.Join(home, ".cache"),
		"XDG_DATA_HOME":               filepath.Join(home, ".local", "share"),
		"XDG_CONFIG_DIRS":             filepath.Join(home, ".config-dirs"),
		"XDG_DATA_DIRS":               filepath.Join(home, ".data-dirs"),
		"MUSTERFLOW_CATALOG_REPO_URL": "",
		"MUSTERFLOW_HOME":             "",
		"MUSTERFLOW_DATA_DIR":         "",
	}
	for name, value := range pinned {
		t.Setenv(name, value)
	}
	return home
}

// resetCLIGlobals zeroes the package globals a sibling test can leak and
// restores whatever it found on cleanup. dashboardBaseURL is the dangerous one:
// when it is non-empty, `connect --tolerate` refuses to run at all, so a leaked
// value turns this surface red for a reason that has nothing to do with it.
func resetCLIGlobals(t *testing.T) {
	t.Helper()
	prevDashboard, prevDataDir, prevOutput := dashboardBaseURL, flagDataDir, outputFlag
	if prevDashboard != "" || prevDataDir != "" || prevOutput != "" {
		t.Logf("ambient CLI globals before this test (dashboard=%q data-dir=%q output-file=%q) — pinned to zero so a sibling's leak cannot change the outcome",
			prevDashboard, prevDataDir, prevOutput)
	}
	dashboardBaseURL, flagDataDir, outputFlag = "", "", ""
	resetAPICommandCache()

	t.Cleanup(func() {
		dashboardBaseURL, flagDataDir, outputFlag = prevDashboard, prevDataDir, prevOutput
		resetAPICommandCache()
	})
}

// chdir moves the process into dir for the duration of the test and restores the
// previous working directory on cleanup (before the temp dir is removed).
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("restore cwd to %s: %v", orig, err)
		}
	})
}

// outboundSink is a loopback HTTP proxy that records and refuses every request
// routed to it. A child process is given it as HTTP_PROXY/HTTPS_PROXY with
// NO_PROXY covering loopback, so the stub stays direct while anything else that
// tries to leave the machine lands here and fails.
type outboundSink struct {
	server *httptest.Server

	mu   sync.Mutex
	seen []string
}

func newOutboundSink(t *testing.T) *outboundSink {
	t.Helper()
	sink := &outboundSink{}
	sink.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sink.mu.Lock()
		if len(sink.seen) < 32 {
			sink.seen = append(sink.seen, r.Method+" "+r.Host)
		}
		sink.mu.Unlock()
		if r.Method == http.MethodConnect {
			http.Error(w, "hermetic CLI test: refused tunnel", http.StatusForbidden)
			return
		}
		http.Error(w, "hermetic CLI test: refused outbound request", http.StatusBadGateway)
	}))
	t.Cleanup(sink.server.Close)
	return sink
}

func (s *outboundSink) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.seen...)
}

// assertEmptyDir fails listing anything found below dir.
func assertEmptyDir(t *testing.T, dir, what string) {
	t.Helper()
	var found []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == dir {
			return nil
		}
		found = append(found, path)
		return nil
	})
	if len(found) > 0 {
		t.Errorf("%s %s was written during the run — the surface resolved state outside its own data dir: %v", what, dir, found)
	}
}

// TestTolerateSurfaceIsHermetic is the INT-CI-202 deliverable: it fails when the
// tolerated-spec surface starts depending on something the repository does not
// pin.
func TestTolerateSurfaceIsHermetic(t *testing.T) {
	t.Run("fixtures live inside the repo and are self-contained", func(t *testing.T) {
		root := repoRoot(t)
		pkg := packageDir(t)
		if rel, err := filepath.Rel(root, pkg); err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("package dir %s is not inside the repository root %s", pkg, root)
		}
		for _, name := range tolerateFixtures {
			path := fixturePath(t, name)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v", name, err)
			}
			for i, line := range strings.Split(string(body), "\n") {
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(trimmed, "$ref:") {
					continue
				}
				if strings.Contains(trimmed, "http://") || strings.Contains(trimmed, "https://") {
					t.Errorf("%s:%d has a remote $ref (%q) — connect would resolve it over the network",
						name, i+1, trimmed)
				}
			}
		}
	})

	t.Run("leaked globals and a foreign cwd cannot change the outcome", func(t *testing.T) {
		stub := newStubAPI(t)

		// Simulate the worst state a sibling test can leave behind BEFORE the
		// harness runs: a dashboard URL makes `connect --tolerate` refuse
		// outright (root.go), so the harness's reset is load-bearing.
		SetDashboardURL("http://127.0.0.1:1")
		t.Cleanup(func() { SetDashboardURL("") })

		pinHermeticEnv(t)
		resetCLIGlobals(t)

		cwd := t.TempDir()
		chdir(t, cwd)

		specPath := fixturePath(t, "vendor-inventory.yaml")
		dataDir := t.TempDir()

		connect := runMF(t, dataDir, "connect", specPath,
			"--tolerate", "--name", "vendor", "--base-url", stub.server.URL)
		if connect.err != nil {
			t.Fatalf("connect --tolerate refused because of a leaked dashboard global: %v\n%s", connect.err, connect.combined())
		}
		call := runMF(t, dataDir, "vendor", "items", "list-items")
		if call.err != nil {
			t.Fatalf("operation call from a foreign cwd failed: %v\n%s", call.err, call.combined())
		}
		if got := stub.hits.Load(); got == 0 {
			t.Fatalf("the stub saw no request — the run never exercised the network path, so the control is vacuous")
		}
		assertEmptyDir(t, cwd, "the test's own working directory")
	})

	t.Run("pristine child process", func(t *testing.T) {
		sink := newOutboundSink(t)
		home := t.TempDir()
		cwd := t.TempDir()

		env := []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + home,
			"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
			"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
			"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
			"TMPDIR=" + t.TempDir(),
			// Any attempt to reach the network outside loopback fails here.
			"HTTP_PROXY=" + sink.server.URL,
			"HTTPS_PROXY=" + sink.server.URL,
			"NO_PROXY=127.0.0.1,localhost,::1",
		}

		cmd := exec.Command(os.Args[0],
			"-test.run=^TestConnectTolerateAcceptedSpecsAreCallable$",
			"-test.timeout=120s",
			"-test.v",
		)
		cmd.Dir = cwd
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("the tolerated-spec test failed in a pristine child process: %v\n%s", err, out)
		}
		// A -test.run that matches nothing exits 0; assert the test really ran.
		if !strings.Contains(string(out), "--- PASS: TestConnectTolerateAcceptedSpecsAreCallable") {
			t.Fatalf("the child process did not run the tolerated-spec test:\n%s", out)
		}
		if left := sink.requests(); len(left) > 0 {
			t.Fatalf("the surface tried to leave the loopback stub: %v\n%s", left, out)
		}
		assertEmptyDir(t, home, "pinned HOME")
		assertEmptyDir(t, cwd, "the child's working directory")
	})

	t.Run("outbound sink refuses non-loopback traffic", func(t *testing.T) {
		sink := newOutboundSink(t)
		proxyURL, err := url.Parse(sink.server.URL)
		if err != nil {
			t.Fatalf("parse sink URL: %v", err)
		}
		client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
		resp, err := client.Get("http://example.invalid/api")
		if err != nil {
			t.Fatalf("probe of a non-stub host did not reach the sink: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("sink answered %d, want %d — the fail-closed path is not installed", resp.StatusCode, http.StatusBadGateway)
		}
		left := sink.requests()
		if len(left) != 1 || !strings.Contains(left[0], "example.invalid") {
			t.Fatalf("sink recorded %v, want exactly one request for example.invalid", left)
		}
	})

	t.Run("engine dependency is pinned by go.mod and go.sum", func(t *testing.T) {
		const engine = "github.com/wojons/muster"
		root := repoRoot(t)

		modData, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil {
			t.Fatalf("read go.mod: %v", err)
		}
		var version string
		for _, line := range strings.Split(string(modData), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == engine {
				version = fields[1]
			}
			if len(fields) >= 3 && fields[0] == "replace" && fields[1] == engine {
				t.Errorf("go.mod replaces the engine with %q — a local replace makes this suite unreproducible; the gitignored go.work is the sanctioned dev-only override", fields[2])
			}
		}
		if version == "" {
			t.Fatalf("go.mod has no require line for %s", engine)
		}

		sumData, err := os.ReadFile(filepath.Join(root, "go.sum"))
		if err != nil {
			t.Fatalf("read go.sum: %v", err)
		}
		needle := engine + " " + version + " h1:"
		if !strings.Contains(string(sumData), needle) {
			t.Fatalf("go.sum has no h1 hash for %s %s — the engine the tests compile against is not verifiable", engine, version)
		}
	})
}
