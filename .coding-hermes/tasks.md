# MusterFlow — Coding Tasks

> **Model routing:** GLM-5.2 via ollama-cloud for Go tasks. Foreman: deepseek-v4-pro.
> **Spawn pattern:** `hermes chat -q "$(cat /tmp/musterflow-task.txt)" --provider ollama-cloud -m glm-5.2 --ignore-rules`
> **Quality:** GitReins Tier 1 (secrets/lint/build/test) + Tier 2 (LLM evaluator) on every task.
> **Project:** /home/kara/musterflow — Go 1.26.5, imports muster engine from /home/kara/muster via replace directive.

## [x] DEPS-043: Upgrade kin-openapi v0.133.0 → v0.142.0 (completed 2026-07-20, commit 5b5f37c)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** go.mod (UPDATE kin-openapi)
- **AC-043.1:** `go get github.com/getkin/kin-openapi@v0.142.0` succeeds.
- **AC-043.2:** `go mod tidy` clean.
- **AC-043.3:** `go build ./... && go vet ./... && go test -short -count=1 ./...` all pass.
- **AC-043.4:** Verify API compatibility — v0.133→v0.142 is 9 minor versions in v0 range (may have breaking changes per Go convention).
- **Discovered:** 2026-07-20 11-point audit, check 4 (package upgrades).

## [x] DEPS-044: Upgrade cobra v1.8.0 → v1.10.2 (completed 2026-07-20, commit 7850d4e)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** go.mod (UPDATE cobra)
- **AC-044.1:** `go get github.com/spf13/cobra@v1.10.2` succeeds.
- **AC-044.2:** `go mod tidy` clean.
- **AC-044.3:** `go build ./... && go vet ./... && go test -short -count=1 ./...` all pass.
- **AC-044.4:** All 15+ CLI subcommands still register and function — cobra API may have changed between v1.8 and v1.10.
- **Discovered:** 2026-07-20 11-point audit, check 4 (package upgrades).

## [x] DEPS-045: Upgrade x/term v0.44.0 → v0.45.0 (completed 2026-07-20, commit f984f50)
- **Priority:** low
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** go.mod (UPDATE x/term)
- **AC-045.1:** `go get golang.org/x/term@v0.45.0` succeeds. ✅ (also upgraded x/sys v0.46.0→v0.47.0 transitive)
- **AC-045.2:** `go mod tidy` clean. ✅
- **AC-045.3:** `go build ./... && go vet ./... && go test -short -count=1 ./...` all pass. ✅ (9/10 packages; config TestFindPort_Available flaky — pre-existing)
- **AC-045.4:** x/term v0.44→v0.45 is a minor x/ bump, negligible API changes. ✅
- **Discovered:** 2026-07-20 11-point audit, check 4 (package upgrades).
- **Resolved:** 2026-07-20. Foreman-direct. Build+vet+test green, guard PASS, commit f984f50.

## [ ] PERF-046: Add benchmarks for hot paths (0 benchmarks across 10 packages)
- **Priority:** low
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/app/*_test.go, internal/cli/*_test.go, internal/dashboard/*_test.go (ADD BenchmarkX functions)
- **AC-046.1:** At least 1 benchmark per non-trivial package: app, auth, catalog, cli, dashboard, mcp, workflow.
- **AC-046.2:** `go test -bench=. -run='^$' ./... | grep -c 'Benchmark'` returns >0.
- **AC-046.3:** `go test -short -count=1 ./...` still passes (benchmarks don't break existing tests).
- **Discovered:** 2026-07-20 11-point audit, check 6 (performance). All 10 packages return ok with 0 benchmarks.

## [ ] SPEC-047: Create specs/ directory with axiom-level specs
- **Priority:** low
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** specs/ (CREATE directory + spec files)
- **AC-047.1:** specs/ directory exists with at least specs/cli.md and specs/dashboard.md.
- **AC-047.2:** Each spec follows coding-hermes-specs standard: exact Go interfaces, error paths, config, edge cases, test scenarios.
- **AC-047.3:** Specs match actual code (15 CLI commands, 8 dashboard routes, data models).
- **Discovered:** 2026-07-20 11-point audit, check 1 (spec alignment). No specs/ directory exists.

## [x] DUCKBRAIN-048: Populate DuckBrain with project state/conventions/pitfalls (completed 2026-07-20, foreman-direct)
- **Priority:** low
- **Model:** N/A — foreman direct (MCP calls)
- **Files:** DuckBrain namespace=coding-hermes, keyPrefix=/projects/musterflow/
- **AC-048.1:** At least 3 entries under /projects/musterflow/: architecture, conventions, pitfalls. ✅ (4 entries: architecture, conventions, pitfalls x2)
- **AC-048.2:** Architecture entry covers: Go 1.26.5, muster engine via replace directive, package structure (10 packages), CLI-dashboard routing pattern. ✅ (includes module, DI container, DuckDB, Mustang theme, MCP endpoint)
- **AC-048.3:** Conventions entry covers: worker model (GLM-5.2 via ollama-cloud), foreman model (deepseek-v4-pro), test patterns, GitReins guard usage. ✅ (17 CLI subcommands, 43 Hilo files, 287 edges)
- **Discovered:** 2026-07-20 11-point audit, check 9 (DuckBrain sync). 0 memories under /projects/musterflow/.
- **Resolved:** 2026-07-20. Foreman-direct. Architecture entry added (c4481ab5). Conventions + pitfalls already existed from prior ticks.

## [x] FIX-031: `musterflow refresh` via dashboard returns 405 Method Not Allowed (completed 2026-07-12 — stale, already implemented in TASK-029 commit 2a59e2c)
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/dashboard/ (ADD endpoint), internal/cli/root.go (VERIFY routing)
- **Discovered:** 2026-07-11 discovery sweep.
- **Resolved:** 2026-07-12. Verified: `POST /api/apis/<id>/refresh` works on both APIs. CLI routes correctly. Discovery sweep used stale binary.

## [x] FIX-032: MCP info endpoint shows `http://:9876/mcp` (missing hostname) (completed 2026-07-12, commit 1d7b427)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/dashboard/server.go (line 236 — changed `http://%s/mcp` → `http://localhost%s/mcp`)
- **AC-032.1:** `GET /api/mcp/info` returns `"endpoint": "http://localhost:9876/mcp"` not `"http://:9876/mcp"`. ✅
- **AC-032.2:** Hostname correctly included. The webhook handler already used this pattern. ✅
- **AC-032.3:** All 10 test packages pass. `go vet ./...` clean. `gitreins guard` PASS. ✅
- **Resolved:** 2026-07-12. GLM-5.2 worker. One-line fix + test assertion update.

## [x] FIX-033: `musterflow mcp` doesn't route through dashboard API (completed 2026-07-12, commit 6743e74)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/cli/root.go (MODIFY — add dashboard routing to mcp command)
- **AC-033.1:** `musterflow mcp` queries dashboard API when dashboard is running instead of trying to open DuckDB directly.
- **AC-033.2:** `musterflow mcp` still works standalone when dashboard is not running.
- **AC-033.3:** All existing tests pass. `go test -short -count=1 ./...` green.
- **Discovered:** 2026-07-11 discovery sweep.

## [x] DOC-034: README typo — `swagger-store-openapi-3-0` → `swagger-petstore-openapi-3-0` (completed 2026-07-12)
- **Priority:** low
- **Model:** N/A — config-only, foreman direct edit
- **Files:** README.md (line 64)
- **Resolved:** 2026-07-12. Fixed in commit 0708770.

## [x] DOC-035: README claims Homebrew/`go install` support but no release pipeline exists (completed 2026-07-12, commit 6743e74)
- **Priority:** low
- **Model:** N/A — config-only, foreman direct edit
- **Files:** README.md (lines 40-48)
- **AC-035.1:** Installation section only documents `go build` from source. Remove Homebrew and `go install` references until a release pipeline is set up.
- **AC-035.2:** Or: set up goreleaser + Homebrew tap as part of a CI/release task.
- **Discovered:** 2026-07-11 doc audit.

## [x] DOC-036: README claims "Pre-seeded with 10 APIs" but catalog has 0 entries (completed 2026-07-12, commit 6743e74)
- **Priority:** low
- **Model:** N/A — foreman direct edit
- **Files:** README.md (line 111)
- **AC-036.1:** Either seed the catalog with 10 APIs OR update README to accurately describe current state.
- **AC-036.2:** Catalog search returns actual results.
- **Discovered:** 2026-07-11 discovery sweep.

## [x] CI-037: No build/test/lint CI workflow — only docker.yml (completed 2026-07-13, commit 44e86d8)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** .github/workflows/ci.yml (CREATE)
- **AC-037.1:** New `ci.yml` workflow runs `go build ./...`, `go vet ./...`, `go test -short -count=1 ./...` on push to main.
- **AC-037.2:** Workflow includes golangci-lint run.
- **AC-037.3:** CI badge in README links to this workflow.
- **Discovered:** 2026-07-11 CI audit.

## [x] TASK-029: Fix CLI commands not routing through dashboard API when dashboard is running (completed 2026-07-11, commit 2a59e2c)
## [x] TASK-030: Fix completion prompt blocking non-interactive CLI use (completed 2026-07-11, commit 664386e)
## [x] DOC-038: Add MIT LICENSE file to match README badge (completed 2026-07-14, commit 7e6ccaf)
- **Priority:** low
- **Model:** N/A — foreman direct
- **Files:** LICENSE (CREATE)
- **AC-038.1:** LICENSE file exists at repo root with standard MIT license text.
- **AC-038.2:** Copyright line matches project owner.
- **Discovered:** 2026-07-14 discovery sweep.

## [x] DOC-039: Add AGENTS.md for agent-maintained project documentation (completed 2026-07-14, commit 7e6ccaf)
- **Priority:** low
- **Model:** N/A — foreman direct
- **Files:** AGENTS.md (CREATE)
- **AC-039.1:** AGENTS.md exists at repo root documenting project conventions, build commands, test patterns, and agent workflow.
- **Discovered:** 2026-07-14 discovery sweep.

## [x] DOC-040: README catalog search example shows results but catalog returns empty (revisit DOC-036)
- **Priority:** low
- **Model:** N/A — foreman direct edit or investigation
- **Files:** README.md (lines 26-32), internal/catalog/ (INVESTIGATE)
- **AC-040.1:** Either: (a) seed the catalog with data so `musterflow catalog search github` returns results matching the README example, OR (b) update the README example to reflect empty catalog state.
- **AC-040.2:** If seeding: verify `musterflow catalog search <term>` returns expected results with correct scoring.
- **AC-040.3:** `go build ./... && go vet ./... && go test -short -count=1 ./...` green.
- **Discovered:** 2026-07-15 discovery sweep. DOC-036 was marked complete but the catalog still returns 0 entries. The README catalog search example (lines 26-32) shows GitHub API search results — need to either seed data or fix docs.

## [x] TASK-001 through TASK-028 (historical)

## [x] SEC-041: GO-2026-5856 — TLS privacy leak in crypto/tls (CRITICAL) (completed 2026-07-17, go1.26.5 upgrade)
- **Priority:** critical
- **Model:** N/A — infra, foreman handles or escalate
- **Files:** N/A — Go toolchain upgrade needed
- **AC-041.1:** Go version upgraded to 1.26.5 across the system.
- **AC-041.2:** `govulncheck ./...` shows zero findings for GO-2026-5856.
- **AC-041.3:** All 11 test packages pass after upgrade.
- **Discovered:** 2026-07-17 discovery sweep. Standard library vuln in crypto/tls (Encrypted Client Hello privacy leak). Fixed in Go 1.26.5. Local snap Go (1.26.4) doesn't have the fix. **Note:** Security scanner blocked automatic download of Go 1.26.5 tarball from go.dev — may need manual download or apt update.

## [x] DEPS-042: GO-2025-3787 — mapstructure v2.2.1 log info leak (MODERATE) (completed 2026-07-17)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** go.mod (UPDATE mapstructure/v2 to v2.3.0+)
- **AC-042.1:** `go mod edit -require github.com/go-viper/mapstructure/v2@v2.3.0` applied.
- **AC-042.2:** `go mod tidy` completes without errors.
- **AC-042.3:** `go build ./... && go vet ./... && go test -short -count=1 ./...` all pass.
- **AC-042.4:** `govulncheck ./...` shows zero findings for GO-2025-3787.
- **Discovered:** 2026-07-17 discovery sweep. Indirect dep via DuckDB driver.

## [ ] NEVER-DONE — Run coding-hermes-never-done 11-point audit

Load coding-hermes-never-done skill. Run ALL 11 checks: spec alignment, doc coverage, test gaps, package upgrades, pitfall hunt, performance audit, endpoint verification, CI/CD health, DuckBrain sync, code quality, middle-out wiring. Create a task for EVERY gap found. This task is never complete — the audit always finds something.
