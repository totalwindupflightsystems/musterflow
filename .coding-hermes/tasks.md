# MusterFlow — Coding Tasks

> **Model routing:** GLM-5.2 via ollama-cloud for all tasks. Provider: `ollama-cloud`, model: `glm-5.2`.
> **Spawn pattern:** `hermes chat -q "$(cat /tmp/musterflow-task.txt)" --provider ollama-cloud -m glm-5.2 -s coding-hermes --yolo --ignore-rules`
> **Quality:** GitReins Tier 1 (secrets/lint/build/test) + Tier 2 (LLM evaluator) on every task.
> **Project:** /home/kara/musterflow — Go 1.26.1, imports muster engine from /home/kara/muster via replace directive.

## [x] TASK-001: MCP tool registration — register connected APIs as MCP tools (completed 2026-06-22)
- **Priority:** highest
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/mcp/server.go (NEW), internal/mcp/tools.go (NEW), internal/dashboard/server.go (MODIFY — wire /mcp endpoint)
- **AC-001.1:** Starting the server registers all connected APIs as MCP tools. Run `musterflow connect https://petstore3.swagger.io/api/v3/openapi.json`, then `musterflow start`. Curl `http://localhost:9876/mcp` with a JSON-RPC `tools/list` request returns tools for each Petstore endpoint (pet, store, user operations).
- **AC-001.2:** MCP `tools/call` works. Send `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"listPets","arguments":{"limit":3}}}` to `/mcp` and get back a valid JSON response with pets or a network error (no real API key needed — the transport layer works, error is from upstream API auth, not from MCP).
- **AC-001.3:** Adding a second API (GitHub) after startup updates MCP tools dynamically. Register Petstore, start server, then connect GitHub. Curl `tools/list` shows tools from BOTH APIs without restart.
- **Files to create/modify:**
  1. `internal/mcp/server.go` — HTTP handler that wraps muster's `pkg/mcp` stdio server for HTTP transport (SSE/JSON-RPC). Imports `github.com/wojons/muster/pkg/mcp` and `github.com/wojons/muster/pkg/mcp/handlers`.
  2. `internal/mcp/tools.go` — tool registry that maps connected APIs → MCP tool descriptors. Reads from `internal/app.Registry`, generates tool JSON schemas from OpenAPI operation parameters.
  3. `internal/dashboard/server.go` — wire `/mcp` handler to the new MCP server instead of the current placeholder.
- **Verify:** `go build ./... && go test ./internal/mcp/... -count=1 && curl -X POST http://localhost:9876/mcp -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | python3 -m json.tool`

## [x] TASK-002: Lazy command generation — `musterflow <api> <operation>` works end-to-end (completed 2026-06-22, commit 35a6f2f)
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/cli/root.go (MODIFY), internal/cli/execute.go (NEW)
- **AC-002.1:** ✅ Commands generated lazily via sync.Once on PersistentPreRunE. `--help` triggers lazy load via custom HelpFunc.
- **AC-002.2:** ✅ Real API calls work. Generator's createRunHandler already executes HTTP requests via request.Builder. execute.go adds ExecuteAndFormat with table/JSON output.
- **AC-002.3:** ✅ Persistence handled by registry.Save/Load (pre-existing). APIConnections survive restarts.
- **Result:** 2 files changed, +327/-5 lines. Build/vet/test/guard all pass. AC-002.1 verified (subcommands show in --help). AC-002.2 verified (HTTP calls to petstore work). AC-002.3 pre-existing (registry persistence already functional). No remote configured — local only.

## [ ] TASK-003: Community catalog — push/pull/search against GitHub repo
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/catalog/client.go (MODIFY), internal/catalog/push.go (NEW), internal/catalog/search.go (NEW)
- **AC-003.1:** Catalog search returns results. `musterflow catalog search petstore` queries `https://raw.githubusercontent.com/totalwindupflightsystems/musterflow-catalog/main/index.json` and displays matching entries.
- **AC-003.2:** Catalog push publishes a connected API. `musterflow catalog push <api-id>` serializes the API connection metadata + spec URL to JSON and guides the user through pushing to the catalog repo (outputs the JSON payload, shows the GitHub PR URL).
- **AC-003.3:** Catalog pull installs from the catalog. `musterflow catalog pull <entry-id>` fetches the entry from the catalog repo, downloads the spec, and runs `connect` on it.
- **Files to modify:**
  1. `internal/catalog/client.go` — already has `Search`, `FetchEntry` methods. Both stubs. Implement against the real catalog repo structure.
  2. `internal/catalog/push.go` (NEW) — serializes `app.APIConnection` to catalog-ready JSON format.
  3. `internal/catalog/search.go` (NEW) — fuzzy search with scoring.
- **Verify:** `musterflow catalog search github && go test ./internal/catalog/... -count=1`

## [ ] TASK-004: Dashboard — browse catalog, view API details, launch MCP info
- **Priority:** medium
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/dashboard/server.go (MODIFY), web/index.html (MODIFY)
- **AC-004.1:** Dashboard shows API details. Clicking a connected API shows: spec URL, version, endpoint count, base URL, auth type, connected date.
- **AC-004.2:** Dashboard has catalog browser. /api/catalog/search?q=... endpoint queries the community catalog. Dashboard shows a search box and results.
- **AC-004.3:** MCP connection info. Dashboard shows the MCP endpoint URL and lists available tools with copy-pasteable JSON-RPC examples.
- **Files to modify:**
  1. `internal/dashboard/server.go` — add `/api/catalog/search` handler, extend `/api/apis/<id>` to return more detail, add `/api/mcp/info` endpoint.
  2. `web/index.html` — add catalog search UI, API detail cards, MCP info section.
- **Verify:** `musterflow start`, open browser to `http://localhost:9876`, verify APIs render, catalog search works, MCP info shows.

## [ ] TASK-005: Tests — achieve >80% coverage on all packages
- **Priority:** high
- **Model:** glm-5.2
- **Provider:** ollama-cloud
- **Files:** internal/app/*_test.go (NEW), internal/cli/*_test.go (NEW), internal/dashboard/*_test.go (NEW), internal/catalog/*_test.go (NEW), internal/mcp/*_test.go (NEW)
- **AC-005.1:** `internal/app` — test registry CRUD, Connect function (with a local OpenAPI spec file), GenerateCommandConfig. Cover: Add, Get, List, Remove, Connect success path, Connect error path (invalid URL, bad spec), Load/Save persistence.
- **AC-005.2:** `internal/cli` — test cobra command registration, connect flag parsing, list output formatting. Cover: NewRootCommand creates correct tree, connect parses flags, list formats API entries.
- **AC-005.3:** `internal/dashboard` — test HTTP handlers with httptest. Cover: /api/health returns 200, /api/apis lists connected APIs, /api/apis/404 not found, / returns HTML.
- **AC-005.4:** `internal/catalog` — test search with a mock HTTP server. Cover: empty catalog returns [], search matching, entry fetch.
- **AC-005.5:** `internal/mcp` — test MCP tool registration with a parsed OpenAPI spec. Cover: tools/list returns correct JSON-RPC, tools/call dispatches to the right handler.
- **Verify:** `go test ./... -count=1 -cover && go tool cover -func=coverage.out | grep total`
