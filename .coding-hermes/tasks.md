# MusterFlow — Coding Tasks

> **Model routing:** GLM-5.2 via ollama-cloud for Go tasks. Foreman: deepseek-v4-pro.
> **Spawn pattern:** `hermes chat -q "$(cat /tmp/musterflow-task.txt)" --provider ollama-cloud -m glm-5.2 --ignore-rules`
> **Quality:** GitReins Tier 1 (secrets/lint/build/test) + Tier 2 (LLM evaluator) on every task.
> **Project:** /home/kara/musterflow — Go 1.26.1, imports muster engine from /home/kara/muster via replace directive.

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

## [ ] FIX-033: `musterflow mcp` doesn't route through dashboard API
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

## [ ] DOC-035: README claims Homebrew/`go install` support but no release pipeline exists
- **Priority:** low
- **Model:** N/A — config-only, foreman direct edit
- **Files:** README.md (lines 40-48)
- **AC-035.1:** Installation section only documents `go build` from source. Remove Homebrew and `go install` references until a release pipeline is set up.
- **AC-035.2:** Or: set up goreleaser + Homebrew tap as part of a CI/release task.
- **Discovered:** 2026-07-11 doc audit.

## [ ] DOC-036: README claims "Pre-seeded with 10 APIs" but catalog has 0 entries
- **Priority:** low
- **Model:** N/A — foreman direct edit
- **Files:** README.md (line 111)
- **AC-036.1:** Either seed the catalog with 10 APIs OR update README to accurately describe current state.
- **AC-036.2:** Catalog search returns actual results.
- **Discovered:** 2026-07-11 discovery sweep.

## [ ] CI-037: No build/test/lint CI workflow — only docker.yml
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
## [x] TASK-001 through TASK-028 (historical)
