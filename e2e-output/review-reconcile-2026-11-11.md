# Review Reconciliation — Carter-LasSalle 2026-11-11 (MusterFlow)

Reconciles `2026-11-11-carter.html` + `2026-11-11-carter-addendum-framing.html` (and
`evidence-musterflow-review-2026-11-11-23-55-25/`) against the current source tree.

- Branch/HEAD: `wt/REVIEW-MUSTERFLOW-20261111` @ `afab6d0` (worktree; product tree identical to origin/master tip `402694b` — delta since `afab6d0` is board-only per commit message).
- Review tested commit: `ca56d348`; addendum source boundary: `351d4ba`. 14 days of fixes have landed since.

---

## AC1 — Board-row reconciliation (live source evidence)

| Row | Verdict | Evidence |
|---|---|---|
| DF-031 — fresh install not reproducible from public clone | **FIXED** (commit `119e6f4`) | `go.mod:10` → `github.com/wojons/muster v0.1.2-0.20261125025543-20f6287d4465` (published pseudo-version); `README.md:32` "The `github.com/wojons/muster` engine is a published Go module, so the build pulls it from the module proxy — no engine checkout is needed". Live: fresh clone build rc=0 (AC2 below). |
| DF-032 — resolver exit-0 semantics unsafe for automation | **PREMISE-DEAD** (re-interpreted by the published-module boundary) | The row's premise was "engine missing ⇒ install silently green then a confusing go build error". The engine is now a published module (`scripts/resolve-engine.sh:4-16`): a missing local checkout is NOT an install failure, so exit 0 with one informational line is the correct semantics. The dangerous branch (`dangling go.work` breaking clones) is handled: `scripts/resolve-engine.sh:33-37` removes a stale `go.work`. Board closed the old contract at `119e6f4` ("build fresh clones against the published muster module"); the current script supersedes the row's PASS criterion ("rc!=0 when engine missing") by moving that failure class out of existence, not by the documented fix. Not a live hazard: a fresh clone never depends on the resolver. |
| DF-030 — workflow engine has NO API-call capability | **FIXED** (commit `23bd641`) | `internal/workflow/builtins.go:59-67` predeclares `http_get`, `http_post`, `http_request`, `sleep`, `json` (the review saw `dsl.NewInterpreter(nil)` with only `trigger`); `engine.go:220-223` passes them as globals; README:154 and docs/workflows.md document them. Live end-to-end probe: AC3. |
| DF-029 — MCP registry goes stale on disconnect | **FIXED** (commit `de0c2a1`) | `internal/dashboard/server.go:152-160` `refreshMCPTools()`; called at `:253` (connect), `:303` (refresh), `:327` (disconnect — the missing call the review flagged), `:487` (import). Test `TestToolRegistry_DynamicTools` PASS. |
| DF-039 — bare operationId collisions / last-connect-wins | **FIXED** | `internal/mcp/tools.go:22-46` namespaced tool names per connected API; dedicated pinning suite `internal/mcp/toolnames_test.go` — 6 tests PASS (AC4). |
| DF-040 — auth split-brain (auth add never attaches to generated calls) | **FIXED** (commit `a6b613c`) | `internal/cli/authbridge.go:16-51` `bridgeGeneratedAuth` hands musterflow-stored credentials to the engine's own resolution path before the request is built; `README.md:141-145` documents `--auth <api-id>`. Wire-level headers pinned by `TestEnsureEngineCredentialAttachesStoredCredential` (AC5). |
| DF-041 — credentials written 0644 | **FIXED** (commit `51cc88a`) | `internal/config/config.go:157-190`: `const configFileMode os.FileMode = 0600`, WriteFile(…0600) + Chmod after write, `warnIfLooseConfigPerms` warning for legacy loose files; README:285 documents 0600. Tests PASS (AC5). |
| DOC-12 — README documents `--token` (rejected by binary) | **FIXED** (commit `a6b613c`) | `README.md:135-136` now `musterflow auth add petstore --type apikey --key <token>` / `… --type bearer --key eyJ…`; live `./musterflow auth add --help` lists `--key`, no `--token` (AC5). |
| DOC-13 — README documents nonexistent `--format` | **FIXED** (commit `a994972`) | `grep -rn -- '--format' README.md docs/ specs/` → 0 hits; live help shows the real flag `--output-file string      Output file path (format auto-detected from extension)`; `internal/cli/execute.go:223` hint now names real flags (`try -o json or --output-file <file>.json instead`). |
| DOC-16 — README says "JSON-RPC over SSE" | **FIXED** (commit `a994972`) | README: 0 SSE hits (`grep -rni 'sse\b\|JSON-RPC over' README.md` → none); `internal/dashboard/index.html:41` → `<p><strong>Transport:</strong> HTTP POST JSON-RPC</p>`. |
| DOC-18 — README claims OAuth2 "with PKCE" while code hard-codes PKCE:false | **FIXED in tree; board row still `pending` (stale row)** | `README.md:148` "Supports: API key, Bearer token, OAuth2 (authorization code, no PKCE), and mTLS." matches `internal/auth/oauth2.go:40,59` `PKCE:         false,`. Docs corrected; only the board row needs closing. |
| GO-TOOLCHAIN-002 — module floor below engine floor | **FIXED** (commit `119e6f4` moves go directive) | `go.mod:3` → `go 1.26.11` (≥ the engine's 1.26.10 floor); commit message: "the go directive moves to 1.26.11 (the engine requires it; this host has go1.26.11)". CI (`.github/workflows/ci.yml:25`, `docker.yml:35`) uses `go-version: '1.26'` (auto-latest ≥ 1.26.11); this host runs go1.26.11 and builds green. |

### Addendum findings (re-weighed against the PRD) — current state

- "Catalog backend 404s live / defaults built but not served" — **PREMISE-DEAD** (rework after judge 5f63d01c; the original 404 verdict was an artifact of probing the wrong filename). Historical probe artifact, kept verbatim: `curl -s -o /dev/null -w "%{http_code}" …catalog.json` → `CATALOG_HTTP=404`. Root cause of the wrong verdict: **catalog.json-vs-entries.json mixup** — the probe hit `catalog.json`, which the repo has never served; the code path actually requests `entries.json`, pinned at `internal/catalog/client.go:25` (`catalogEntriesFile = "entries.json"`; the old index.json mistake is documented at client.go:19-23, with index.json kept only as a fallback for operator-pointed mirrors). Live re-verification (this rework, both arms):
  - `curl -s -o /dev/null -w "%{http_code}" https://raw.githubusercontent.com/totalwindupflightsystems/musterflow-catalog/main/entries.json` → `HTTP 200`, and the body contains **10 real entries** (first: `github` / GitHub). Same URL with `catalog.json` → `HTTP 404`, confirming the mixup.
  - `go build -o bin/musterflow ./cmd/musterflow` → `BUILD_RC=0`, then `./bin/musterflow --data-dir /tmp/mf-rework-cat catalog search github` → rc=0, GitHub row served from the live backend, **no "serving bundled seed entries" notice**.
  - Control arm: `MUSTERFLOW_CATALOG_REPO_URL=https://127.0.0.1:1/none ./bin/musterflow --data-dir /tmp/mf-rework-cat catalog search github` → rc=0 and the notice DOES print (`notice: community index unavailable — serving bundled seed entries … connection refused`), proving the bundled-seed fallback arms correctly.
  `catalog/seed.json` still contains exactly the ten PRD APIs (`jq` → github, stripe, slack, discord, openai, notion, linear, jira, twilio, cloudflare) and now serves only when an override backend is unreachable. No brew channel; still Phase 2 per PRD.
- "Seed/config defaults are real and sensible" — **CONFIRMED**: seed file above; `internal/config/config.go` defaults unchanged (port 9876 / table / auto-completion).

## AC2 — Fresh clone builds from the published dependency

Fresh clone of `/home/kara/musterflow` (commit `402694b`, board-only delta from this worktree's `afab6d0`), NO private engine mount anywhere: host env checked clean (`echo $GOWORK $MUSTER_ENGINE_DIR $GOFLAGS` → all empty; `/home/kara/wojons/muster` exists on disk but was never referenced — no `go.work` present, resolve-engine.sh not run before build).

```
$ git clone -q /home/kara/musterflow /tmp/mf-reviewr8-fresh-24303   # exit 0
$ git -C /tmp/mf-reviewr8-fresh-24303 log --oneline -1
402694b board(musterflow): tick 2026-11-25-15-43-56 …
$ cd /tmp/mf-reviewr8-fresh-24303
$ ls go.work                      → "No such file or directory"
$ CGO_ENABLED=1 make build        → exit 0 (BUILD_RC=0)
  go build -o bin/musterflow -ldflags "-X main.Version=$(git describe --always --dirty)" ./cmd/musterflow
$ go list -m -f '{{.Dir}}' github.com/wojons/muster
/mnt/ssd/caches/hermes-go-mod/github.com/wojons/muster@v0.1.2-0.20261125025543-20f6287d4465
```

The engine resolves from the module proxy/cache as a published requirement (`go.mod` untouched; no workspace replace). **Verdict: VERIFIED** — a clone without any private checkout builds bin/musterflow from the released dependency.

Resolver failure semantics (dev-only path):

```
$ bash scripts/resolve-engine.sh
info: no local muster engine checkout — building against the published muster module (set MUSTER_ENGINE_DIR to develop against a local checkout)
$ echo $?                          → 0  (correct: no checkout is not an install failure)
$ bash scripts/resolve-engine.sh; ls go.work   → still "No such file or directory" (ls rc=2; no workspace written)
```

The resolver's real failure branch (`go.mod` missing) still exits 1 (`scripts/resolve-engine.sh:37-40`). A resolver/dependency failure in the install-critical path is now a hard `go build` error (nonzero) since `go.mod` is authoritative — `go build` rc=1 is the resolver-exit-nonzero guarantee (not re-faked in dev-only script). **Minor doc-level nit (not a code bug):** if `MUSTER_ENGINE_DIR` is set but the path contains no `go.mod`, the script ignores it and prints the generic "no local checkout" info line (rc 0) instead of naming the invalid path.

## AC3 — Workflow calls a connected local HTTP fixture + failure path

Built a probe (verification-only `internal/workflow/zz_review_probe_test.go`, run then deleted — never committed) driving the real `Engine.Run` + real `http_get` builtin against a stdlib Go `httptest` fixture:

- Happy path: flow script = `resp = http_get("<fixture>/widget/42", {"Accept":"application/json"}); body = json.decode(resp["body"]); print("status="+str(resp["status"])+" source="+body["source"])` — fixture asserted it was hit at `/widget/42`; output asserted `status=200 source=probe-fixture`.
- Failure path: `http_get` against a bound-then-closed localhost port → `Run` returns an error; asserted the error names the failing builtin and the clean refused-connection failure (`http_get … connection refused`).

```
$ go test ./internal/workflow -count=1 -run 'TestReviewProbe' -v
--- PASS: TestReviewProbeFlowCallsLocalFixture (0.00s)
--- PASS: TestReviewProbeRefusedConnectionRaisesCleanError (0.00s)
PASS
ok  github.com/totalwindupflightsystems/musterflow/internal/workflow   0.007s   (exit 0)
```

Repo's own regression suite for the same path also passes (`TestRun_JSONDecode`, `TestRun_JSONEncode`, etc. — package `internal/workflow` ok in AC6).

**Starlark builtin surface documentation: DOCUMENTED.** `docs/workflows.md` (authoring reference: full builtin table with exact signatures and failure modes — `http_get`/`http_post`/`http_request` at lines 218-220, five predeclared globals at 197-198); `docs/integration-guide.md:487` ("HTTP Builtins" section) and `README.md:154-171`. Not missing.

**Verdict: VERIFIED.**

## AC4 — Duplicate operationIds → unique MCP tool names; disconnect reflects surviving registry

Existing tests in `internal/mcp/toolnames_test.go` (file header pins DF-039) cover exactly this surface:

```
$ go test ./internal/mcp -count=1 -run 'Colliding|Disconnect|Dynamic|Duplicate' -v
--- PASS: TestToolRegistry_DynamicTools                    (0.03s)
--- PASS: TestRefresh_CollidingOperationIDs_NoDuplicateNames (0.05s)   # two APIs, same operationId → no duplicate names; per-API slugs
--- PASS: TestExecute_CollidingOperationIDs_RoutesPerAPI     (0.04s)   # tools/call namespaced name routes to the right API base URL
--- PASS: TestRefresh_DisconnectKeepsRouting               (0.04s)   # after disconnect of one API, Refresh() re-derives: only the survivor's tools, bare-name alias released
--- PASS: TestHTTPServer_ToolsList_NoDuplicateNames         (0.05s)   # wire-level tools/list
--- PASS: TestHTTPServer_ToolsCall                          (see full run) PASS
ok  github.com/totalwindupflightsystems/musterflow/internal/mcp  0.244s
```

(Exit code for the same selector in the AC6 full run: `TEST_RC=0`). **Verdict: VERIFIED.**

## AC5 — Auth attachment, 0600 config, documented flags vs live help

- **Auth attached on the generated request** — pinned in-repo: `internal/auth/engine_test.go:76 TestEnsureEngineCredentialAttachesStoredCredential` proves a credential stored by `musterflow auth add` (config.yaml `auth:` map) reaches the engine's own header-resolution path with the real wire headers: apikey → `X-API-Key: LIVEKEY`, bearer/oauth2 → `Authorization: Bearer …`, and is rejected with the real-flags hint otherwise (lines 131-141: `musterflow auth add probe --type apikey --key`). The bridge wraps every generated command carrying `--auth` (`internal/cli/authbridge.go:16-51`); README:141-145 matches.

```
$ go test ./internal/auth -count=1 -run 'EnsureEngineCredential' -v
--- PASS: TestEnsureEngineCredentialAttachesStoredCredential (0.00s)
--- PASS: TestEnsureEngineCredentialNotFoundHintUsesRealFlags (0.00s)
--- PASS: TestEnsureEngineCredentialLeavesEngineCredentialAlone (0.00s)
--- PASS: TestEnsureEngineCredentialRejectsMTLS                (0.00s)
PASS  ok  internal/auth  (exit 0)
```

- **Config files 0600** — write path `internal/config/config.go:157` (`const configFileMode os.FileMode = 0600`), `:184 WriteFile(…, configFileMode)` + `Chmod(path, configFileMode)` for legacy loose files + `warnIfLooseConfigPerms` stderr notice; regression tests pass:

```
$ go test ./internal/config -count=1 -run 'TestSaveWithDataDir_FileMode0600|TestSaveWithDataDir_Preserves0600OnRewrite' -v
--- PASS: TestSaveWithDataDir_FileMode0600 (0.00s)
--- PASS: TestSaveWithDataDir_Preserves0600OnRewrite (0.00s)
PASS  ok  internal/config  (exit 0)
```

- **Documented flags vs live help** — built `./musterflow` from the worktree; `./musterflow --help` (rc 0):
  - `--key string  API key or bearer token` present on `auth add`; **no `--token` flag** (DOC-12 match).
  - `--output-file string  Output file path (format auto-detected from extension)` present; **no `--format` flag anywhere** (DOC-13 match).
  - `--retries int` matches recent DF-044 semantics; `--data-dir`, `--dashboard-addr`, `--no-dashboard`, `-v` as documented.
  - Auth-type surface in live help: "Auth type: none, apikey, bearer, oauth2, mtls" matches `internal/auth/oauth2.go` and README:148's "no PKCE" (DOC-18 match).
  - README MCP descriptions are HTTP POST JSON-RPC only (DOC-16 match).
  - No mismatches found between the reconciled doc claims and live help/source.
  - Note: the AC's literal `go run . --help` is the wrong invocation for this layout (`main` lives in `cmd/musterflow`; root `go run .` → "no Go files in …" rc=1). That is expected repo shape (README and Makefile document `./cmd/musterflow/`), not a doc mismatch — Makefile `build` target is the front door and produced the binary used here.

**Verdict: VERIFIED** (live help + source agree; no quoted mismatches).

## AC6 — `go build ./... && go test ./...` in the worktree

```
$ cd /home/kara/worktrees/musterflow-REVIEW-MUSTERFLOW-20261111
$ go build ./...                         → BUILD_RC=0
$ go test ./...                          → TEST_RC=0, 13 packages ok:
ok  cmd/musterflow     2.025s
ok  internal/app       3.589s
ok  internal/auth      0.262s
ok  internal/catalog   0.039s
ok  internal/cli       8.571s
ok  internal/completion 0.024s
ok  internal/config    0.021s
ok  internal/dashboard 3.332s
ok  internal/mcp       1.987s
ok  internal/retrypin  0.004s
ok  internal/specfix   0.082s
ok  internal/wasm      2.909s
ok  internal/workflow  1.393s
```

Test census: `go test -count=1 -list '.*' ./... | grep -c '^Test'` → **631 test functions** across the module (full output archived in the run log `/home/kara/.hermes/cache/scratch/mf_ac6_full.txt`). **Verdict: VERIFIED.**

## NEW FINDINGS

No new live code bugs surfaced in AC2/AC4 (fresh-clone build and MCP registry/disconnect suites are green). Two minor, doc-level observations (not code defects; no fix applied per instructions):

1. `scripts/resolve-engine.sh` — when `MUSTER_ENGINE_DIR` is set to a path without a `go.mod`, the script silently ignores it and prints the generic "no local checkout" info line (rc 0). A one-line stderr note naming the invalid path would help engine devs. (`scripts/resolve-engine.sh:57-59`.)
2. `go run . --help` cannot work in a root that holds no `main` package (`cmd/musterflow/` is the entry). Help/help-adjacent docs that show `go run` examples should show `go run ./cmd/musterflow`. (Documented shape; README already shows `go build -o … ./cmd/musterflow/`.)

Stale board row worth closing by the foreman (bookkeeping only): **DOC-18** is `pending` while its fix is in the tree (`README.md:148` "no PKCE" matches `internal/auth/oauth2.go:40,59`).
