
## Dogfood Findings (2026-09-01)
Verdict: PROMISING-BUT-ROUGH
Promise: {"entry_point":"CLI binary `musterflow` (Go/cobra, cmd/musterflow/main.go) — the same binary also starts the dashboard + MCP + REST API server on :9876 via `musterflow start`","promise":"This project claims a user can turn any OpenAPI spec into instant tooling: connect a spec URL (or local file) onc

- [P1] README flagship example is not reproducible today — find-pets-by-status --status available returns 'HTTP error: 500' with no hint the upstream is down; direct curl proved petstore3.swagger.io itself 500s on all pet endpoints (even GET of a nonexistent 
- [P1] MCP tool registry goes stale on disconnect, needs restart — After DELETE /api/apis/<id>, /api/apis correctly shows 1 API but tools/list still lists 38 tools (stale duplicates); only a restart refreshes it. This breaks the promise 'tools update without restart'
- [P2] Generated CLI flag shapes are inconsistent across commands — get-pet-by-id takes a positional <petId> while add-pet exposes body fields as --flags; a guessed --petId flag is rejected and the shapes are only discoverable via --help. Confusing for a tool whose wh
- [P2] GOWORK=off env produces a cryptic build failure the docs never mention — go build fails with 'missing go.sum entry for ... github.com/wojons/muster/pkg/...' when GOWORK=off is set (stale env var on the host); unsetting GOWORK fixes it, but resolve-engine.sh docs give no wa
- [P2] Catalog search error diverges from README when dashboard is running — catalog search with the dashboard up returns 'dashboard returned HTTP 500' while the README documents 'catalog backend not available' (404) for the same command — the two routing modes surface differe

## Dogfood Findings (2026-09-04)
Verdict: PROMISING-BUT-ROUGH
Promise: {"entry_point":"Single Go CLI binary  (cmd/musterflow/main.go, Cobra command tree);  launches the HTTP dashboard + REST API + MCP JSON-RPC server + webhook hooks, all on port 9876 (with 9877-9886 auto-discovery). Build depends on the private github.com/wojons/muster eng

- [P1] README quickstart dead for two consecutive runs; no documented fallback — Petstore3 upstream returns HTTP 500 on ALL operations (direct curl /pet/findByStatus and /store/inventory both 500), so the flagship find-pets-by-status example is unreproducible for a second straight
- [P1] Structured log lines pollute stdout and break --output json piping — 'INF HTTP response received' log lines land on stdout; python json.load fails with 'Extra data' until stderr is redirected. Breaks the clean-stdout contract needed for CLI automation into jq/python co
- [P2] Request-body flags demand undocumented JSON-encoded values — --body 'created via CLI' fails with 'invalid JSON for --body: value is not valid JSON'; correct form is --body '"created via CLI"' and that requirement is nowhere documented — first create operation t
- [P2] Inconsistent flag/positional shapes across subcommands, discoverable only via --help — flow run wf-math works but flow run --name wf-math errors 'unknown flag: --name' while flow create uses --name; auth add --api-key sk-... errors (real shape is --type apikey --key); catalog search --q
- [P2] Undocumented operational pitfalls: DuckDB lock conflict and catalog error divergence — --no-dashboard against the same data-dir as a running server fails with 'Conflicting lock is held in .../musterflow.db' (separate data-dir needed, undocumented). Catalog search surfaces a dashboard-ro


## Dogfood Findings (2026-09-07)
Verdict: PROMISING-BUT-ROUGH
Promise: {"entry_point":"CLI binary `musterflow` (Go/Cobra, cmd/musterflow/main.go) — also hosts the HTTP dashboard, REST API, MCP JSON-RPC endpoint, and webhooks on port 9876 via `musterflow start`","promise":"MusterFlow claims a user can turn any OpenAPI spec into an instant CLI with subcommands for every endpoint, an MCP tool, and a workflow engine"}

- [P1] README quickstart petstore3 unreproducible — upstream 500 with no documented fallback — Verified live: https://petstore3.swagger.io/api/v3/openapi.json returns 200 (connect works, ID 715e581e0d464caa, 19 endpoints) but the quickstart's find-pets-by-status call returns HTTP 500 from upstream (3rd consecutive run). README offers no fallback/mock; docs/integration-guide.md's 127.0.0.1:18099 examples have no runnable mock recipe in the repo (grep for 18099 finds only docs + .gitreins history).
- [P1] MCP tools go stale after disconnect — DELETE leaves tools registered until restart — Verified live: after `musterflow disconnect 9e147e1a050203c8` with server running, GET /api/apis shows 0 APIs but POST /mcp tools/list still returns 4 tools (getNote, deleteNote, listNotes, createNote). Dynamic ADD works; DELETE does not refresh the tool registry.
- [P1] Inconsistent flag shapes across subcommands — Verified live: `flow create --name jf1` works but `flow run --name jf1` fails with 'unknown flag: --name' (positional only, cobra.ExactArgs(1)); `catalog search --query pet` fails with 'unknown flag: --query' (positional only). Same-verb families accept different flag styles.
- [P1] catalog search error diverges when dashboard is running — Verified live: with server on :9876, `catalog search pet` → 'Error: dashboard returned HTTP 500' (dashboard handleCatalogSearch maps catalog client error to 500); without dashboard, local mode returns the clear 'catalog backend not available (HTTP 404): repo absent or private'. Same operation, two very different failure surfaces.
- [P2] --body semantics undocumented: JSON-encoded, replaces whole body, per-property flags ignored — Verified live: `create-note --title ignored --body '{"title":"body wins"}'` sends only the raw body (per-property flags silently dropped — DF-MUSTER-5 design); raw string `--body 'not json'` errors 'invalid JSON for --body' with no README/help documentation of either behavior. Report friction #4 (INF logs on stdout breaking JSON pipes) NOT reproducible: zerolog ConsoleWriter targets os.Stderr and `--output json | python3 -m json.tool` succeeds with INF captured in stderr — dropped.
