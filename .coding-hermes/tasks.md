# MusterFlow — Coding Tasks

> **Model routing:** GLM-5.2 via ollama-cloud for Go tasks. Foreman: deepseek-v4-pro.
> **Spawn pattern:** `hermes chat -q "$(cat /tmp/musterflow-task.txt)" --provider ollama-cloud -m glm-5.2 --ignore-rules`
> **Quality:** GitReins Tier 1 (secrets/lint/build/test) + Tier 2 (LLM evaluator) on every task.
> **Project:** /home/kara/musterflow — Go 1.26.1, imports muster engine from /home/kara/muster via replace directive.

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

## [ ] TASK-031: Connect 4 APIs to prove product value — GitHub, Stripe, Linear, Slack
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/cli/root.go (read-only), internal/app/connect.go (read-only), catalog/seed.json (MODIFY)
- **AC-031.1:** Connect GitHub with a real fine-grained token. `musterflow auth add gh --type bearer --key <token>` → `musterflow gh user get` returns actual GitHub user data.
- **AC-031.2:** Connect Stripe in test mode. `musterflow connect https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.json --name stripe` → `musterflow stripe balance get` works with test key.
- **AC-031.3:** Connect Linear. `musterflow connect https://raw.githubusercontent.com/linear/linear/master/packages/sdk/src/schema.graphql --name linear` — note Linear may need GraphQL→OpenAPI conversion or a community spec.
- **AC-031.4:** Connect Slack for notifications. `musterflow connect https://raw.githubusercontent.com/slackapi/slack-api-specs/master/web-api/slack_web_openapi_v2.json --name slack` → `musterflow slack chat postMessage --channel test --text "Hello from MusterFlow"` works.
- **AC-031.5:** All 4 APIs show up in `musterflow list` with correct endpoint counts. `go test -short -count=1 ./...` passes.
- **Verify:** `musterflow list | grep -E "gh|stripe|linear|slack"` shows all 4, each with endpoint count > 0.

## [ ] TASK-032: Catalog CI validation — GitHub Action for PR quality gates
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** .github/workflows/catalog-ci.yml (NEW)
- **AC-032.1:** GitHub Action triggers on PR to totalwindupflightsystems/musterflow-catalog main branch.
- **AC-032.2:** Validates entries.json against JSON schema (must have id, name, type, spec_url, description fields).
- **AC-032.3:** Fetches each spec_url and runs OpenAPI validation (go run muster's openapi parser).
- **AC-032.4:** Computes quality scores (domain reputation + spec structure) and posts as PR comment.
- **AC-032.5:** Action file committed to musterflow-catalog repo's .github/workflows/ directory.

## [ ] TASK-033: Docker integration smoke test — build, start, health check, connect, execute
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** tests/smoke.sh (NEW), Dockerfile (read-only)
- **AC-033.1:** Smoke script builds Docker image, starts container, waits for health check.
- **AC-033.2:** Inside container: `musterflow connect https://petstore3.swagger.io/api/v3/openapi.json` → success.
- **AC-033.3:** Inside container: `musterflow petstore listPets --limit 3` → returns HTTP response (200 or auth error, not transport error).
- **AC-033.4:** Script cleans up container and reports pass/fail exit code.
- **Verify:** `bash tests/smoke.sh` returns exit 0.

## [ ] TASK-034: End-to-end demo — 2 mock APIs + Starlark flow + webhook trigger
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** tests/e2e/ (NEW directory with mock servers + test runner)
- **AC-034.1:** Two httptest mock API servers (Stripe-like webhook sender + GitHub-like issue tracker).
- **AC-034.2:** Connect both mock APIs to MusterFlow.
- **AC-034.3:** Create a Starlark flow: `on stripe:charge.succeeded → gh issues create(title="Payment: $amount")`.
- **AC-034.4:** Trigger the webhook → verify the flow executes → verify the GitHub issue was "created" (logged by mock server).
- **AC-034.5:** Test script returns exit 0 on success, exit 1 on failure.
- **Verify:** `go test -count=1 -v ./tests/e2e/...` passes.
