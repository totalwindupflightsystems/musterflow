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
- **AC-031.1:** `musterflow refresh <api-id>` works when dashboard is running. Currently returns "refresh via dashboard: method not allowed" because `/api/apis/<id>/refresh` endpoint doesn't exist.
- **AC-031.2:** Dashboard adds a `POST /api/apis/<id>/refresh` endpoint that re-fetches the OpenAPI spec and regenerates commands.
- **AC-031.3:** All existing tests pass. `go test -short -count=1 ./...` green.
- **Discovered:** 2026-07-11 discovery sweep. CLI routes through dashboard (TASK-029) but dashboard has no refresh endpoint.
- **Resolved:** 2026-07-12. Verified: `POST /api/apis/<id>/refresh` works on both APIs. `musterflow refresh <id>` routes through dashboard correctly. Discovery sweep had used stale binary.

## [ ] FIX-032: MCP info endpoint shows `http://:9876/mcp` (missing hostname)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/dashboard/server.go (line 236 — change `http://%s/mcp` → `http://localhost%s/mcp`)
- **AC-032.1:** `GET /api/mcp/info` returns `"endpoint": "http://localhost:9876/mcp"` not `"http://:9876/mcp"`.
- **AC-032.2:** The endpoint URL correctly includes the hostname regardless of which interface the server binds to.
- **AC-032.3:** All existing tests pass. `go test -short -count=1 ./...` green.
- **Discovered:** 2026-07-11 discovery sweep. Dashboard shows correct URL in HTML but API endpoint drops the host.

## [ ] FIX-033: `musterflow mcp` doesn't route through dashboard API
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/cli/root.go (MODIFY — add dashboard routing to mcp command)
- **AC-033.1:** `musterflow mcp` queries dashboard API when dashboard is running instead of trying to open DuckDB directly. Should call `GET /api/mcp/info` and display tool count and endpoint URL.
- **AC-033.2:** `musterflow mcp` still works standalone when dashboard is not running (existing behavior with direct DuckDB access).
- **AC-033.3:** All existing tests pass. `go test -short -count=1 ./...` green.
- **Discovered:** 2026-07-11 discovery sweep. mcp command shows "No APIs connected" despite dashboard having 2 APIs. Unlike list/catalog, it doesn't route through dashboard.

## [ ] DOC-034: README typo — `swagger-store-openapi-3-0` → `swagger-petstore-openapi-3-0`
- **Priority:** low
- **Model:** N/A — config-only, foreman direct edit
- **Files:** README.md (line 64)
- **AC-034.1:** Line 64 uses correct subcommand name `swagger-petstore-openapi-3-0` matching the actual connected API.
- **Discovered:** 2026-07-11 doc audit. Quick Start example uses wrong API name.

## [ ] DOC-035: README claims Homebrew/`go install` support but no release pipeline exists
- **Priority:** low
- **Model:** N/A — config-only, foreman direct edit
- **Files:** README.md (lines 40-48)
- **AC-035.1:** Installation section only documents `go build` from source. Remove Homebrew and `go install` references until a release pipeline is set up.
- **AC-035.2:** Or: set up goreleaser + Homebrew tap as part of a CI/release task.
- **Discovered:** 2026-07-11 doc audit. `brew install musterflow` and `go install ...@latest` are aspirational, not functional.

## [ ] DOC-036: README claims "Pre-seeded with 10 APIs" but catalog has 0 entries
- **Priority:** low
- **Model:** N/A — foreman direct edit
- **Files:** README.md (line 111)
- **AC-036.1:** Either seed the catalog with 10 APIs OR update README to accurately describe current state.
- **AC-036.2:** Catalog search returns actual results.
- **Discovered:** 2026-07-11 discovery sweep. `catalog search` returns 0 results. README claims pre-seeded catalog.

## [ ] CI-037: No build/test/lint CI workflow — only docker.yml
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** .github/workflows/ci.yml (CREATE)
- **AC-037.1:** New `ci.yml` workflow runs `go build ./...`, `go vet ./...`, `go test -short -count=1 ./...` on push to main.
- **AC-037.2:** Workflow includes golangci-lint run.
- **AC-037.3:** CI badge in README links to this workflow.
- **Discovered:** 2026-07-11 CI audit. Only `docker.yml` exists, triggered on tag push only. No code quality verification on main.

## [x] TASK-029: Fix CLI commands not routing through dashboard API when dashboard is running (completed 2026-07-11, commit 2a59e2c)
## [x] TASK-030: Fix completion prompt blocking non-interactive CLI use (completed 2026-07-11, commit 664386e)
## [x] TASK-001 through TASK-028 (historical)
