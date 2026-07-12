# MusterFlow — Coding Tasks

> **Model routing:** GLM-5.2 via ollama-cloud for Go tasks. Foreman: deepseek-v4-pro.
> **Spawn pattern:** `hermes chat -q "$(cat /tmp/musterflow-task.txt)" --provider ollama-cloud -m glm-5.2 --ignore-rules`
> **Quality:** GitReins Tier 1 (secrets/lint/build/test) + Tier 2 (LLM evaluator) on every task.
> **Project:** /home/kara/musterflow — Go 1.26.1, imports muster engine from /home/kara/muster via replace directive.

## [ ] FIX-031: `musterflow refresh` via dashboard returns 405 Method Not Allowed
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/dashboard/ (ADD endpoint), internal/cli/root.go (VERIFY routing)
- **AC-031.1:** `musterflow refresh <api-id>` works when dashboard is running. Currently returns "refresh via dashboard: method not allowed" because `/api/apis/<id>/refresh` endpoint doesn't exist.
- **AC-031.2:** Dashboard adds a `POST /api/apis/<id>/refresh` endpoint that re-fetches the OpenAPI spec and regenerates commands.
- **AC-031.3:** All existing tests pass. `go test -short -count=1 ./...` green.
- **Discovered:** 2026-07-11 discovery sweep. CLI routes through dashboard (TASK-029) but dashboard has no refresh endpoint.

## [ ] FIX-032: MCP info endpoint shows `http://:9876/mcp` (missing hostname)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/dashboard/ (FIX endpoint URL construction)
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
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/cli/root.go (MODIFY — add dashboard routing to list, catalog, refresh commands)
- **AC-029.1:** `musterflow list` works when the dashboard is running. Should call `GET /api/apis` instead of `registry.List()` when `dashboardBaseURL` is set. Currently returns "No APIs connected" because LoadReadOnly() fails with DuckDB lock conflict.
- **AC-029.2:** `musterflow catalog search <query>` works when dashboard is running. Should call `GET /api/catalog/search?q=<query>` instead of loading catalog directly.
- **AC-029.3:** `musterflow refresh <id>` works when dashboard is running. Should call `POST /api/apis/<id>/refresh` or equivalent dashboard endpoint.
- **AC-029.4:** `musterflow catalog push` and `catalog pull` work when dashboard is running.
- **AC-029.5:** All existing test packages remain green. `go test -short -count=1 ./...` passes.
- **Verify:** Start dashboard in background, run `musterflow list`, verify 2 connected APIs shown (GitHub v3, Petstore).
- **Discovered:** 2026-07-11 discovery sweep. Dashboard shows 2 APIs via curl, but CLI reports "No APIs connected" because newListCommand calls registry.List() directly instead of routing through dashboard API. connect/disconnect already route correctly — list, catalog, and refresh need the same pattern.

## [x] TASK-030: Fix completion prompt blocking non-interactive CLI use (completed 2026-07-11, commit 664386e)
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/completion/install.go (MODIFY), cmd/musterflow/main.go (MODIFY)
- **AC-030.1:** CLI commands work without blocking when stdin is not a TTY. Currently `PromptInstall` reads from stdin and blocks when piped (e.g., `echo 'N' | musterflow list`). Should auto-detect non-TTY and skip the prompt.
- **AC-030.2:** Shell completion still prompts interactively when in a TTY (existing behavior preserved).
- **AC-030.3:** All existing tests pass. `go test -short -count=1 ./...` green.
- **Discovered:** 2026-07-11 discovery sweep. `musterflow list` hung waiting for Y/n input on shell completion prompt. Had to pipe `echo 'N' |` to unblock.

## [x] TASK-001: MCP tool registration — register connected APIs as MCP tools (completed 2026-06-22)
## [x] TASK-002: Lazy command generation — `musterflow <api> <operation>` works end-to-end (completed 2026-06-22, commit 35a6f2f)
## [x] TASK-003: Community catalog — push/pull/search against GitHub repo (completed 2026-06-22)
## [x] TASK-004: Dashboard — browse catalog, view API details, launch MCP info (completed 2026-06-22)
## [x] TASK-005: Tests — achieve >80% coverage on all packages (7/8 done)
## [x] TASK-006: Config system — YAML config, port auto-discovery, data directory (completed 2026-06-23)
## [x] TASK-007: Auth per API — apikey, bearer, oauth2, mTLS credential management (completed 2026-06-24)
## [x] TASK-008: Output formats — CSV, JSONL, Parquet + format auto-detection (completed 2026-06-24)
## [x] TASK-009: Shell completion — bash, zsh, fish auto-install (completed 2026-06-24)
## [x] TASK-010: Docker multi-arch image — linux/amd64 + linux/arm64 (completed 2026-06-24)
## [x] TASK-011: Landing page — musterflow.com static site (GitHub Pages) (completed 2026-06-24)
## [x] TASK-012: DuckDB + JSONL storage — replace JSON registry file (completed 2026-06-24)
## [x] TASK-013: Spec refresh — manual + scheduled refresh of API specs (completed 2026-06-24)
## [x] TASK-014: Catalog quality scoring — automated tier assignment (completed 2026-06-24)
## [x] TASK-015: WASM transform infrastructure (completed 2026-06-24)
## [x] TASK-016: Catalog seeding — 10 most annoying APIs (completed 2026-06-24)
## [x] TASK-017: Workflow engine — Starlark DSL + webhook triggers (completed 2026-06-24)
## [x] TASK-018: Auth test coverage — YAMLTokenStore + OpenBrowser tests
## [x] TASK-019: App test coverage — fill app coverage gap to >80% (completed 2026-06-24)
## [x] TASK-020: Workflow engine tests — NewEngine, Create, List, Run (>80% coverage) (completed 2026-06-25)
## [x] TASK-021: cli coverage — test command constructors (completed 2026-06-25)
## [x] TASK-022: wasm coverage — test Registry and stub functions (completed 2026-06-25)
## [x] TASK-023: cli command-constructor tests — config, auth, refresh, flow, transform (completed 2026-06-25)
## [x] TASK-024: cli command-constructor tests — catalog (completed 2026-06-25)
## [x] TASK-025: cli coverage — test actionable RunE gaps (target >75%) (completed 2026-06-28)
## [x] TASK-026: Fix DuckDB lock conflict — CLI unusable while dashboard is running (completed 2026-07-10)
## [x] TASK-027: Create README.md
## [x] TASK-028: Fix pre-existing errcheck lint warnings
