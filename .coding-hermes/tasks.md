
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

