
## Dogfood Findings (2026-09-01)
Verdict: PROMISING-BUT-ROUGH
Promise: {"entry_point":"CLI binary `musterflow` (Go/cobra, cmd/musterflow/main.go) — the same binary also starts the dashboard + MCP + REST API server on :9876 via `musterflow start`","promise":"This project claims a user can turn any OpenAPI spec into instant tooling: connect a spec URL (or local file) onc

- [P1] README flagship example is not reproducible today — find-pets-by-status --status available returns 'HTTP error: 500' with no hint the upstream is down; direct curl proved petstore3.swagger.io itself 500s on all pet endpoints (even GET of a nonexistent 
- [P1] MCP tool registry goes stale on disconnect, needs restart — After DELETE /api/apis/<id>, /api/apis correctly shows 1 API but tools/list still lists 38 tools (stale duplicates); only a restart refreshes it. This breaks the promise 'tools update without restart'
- [P2] Generated CLI flag shapes are inconsistent across commands — get-pet-by-id takes a positional <petId> while add-pet exposes body fields as --flags; a guessed --petId flag is rejected and the shapes are only discoverable via --help. Confusing for a tool whose wh
- [P2] GOWORK=off env produces a cryptic build failure the docs never mention — go build fails with 'missing go.sum entry for ... github.com/wojons/muster/pkg/...' when GOWORK=off is set (stale env var on the host); unsetting GOWORK fixes it, but resolve-engine.sh docs give no wa
- [P2] Catalog search error diverges from README when dashboard is running — catalog search with the dashboard up returns 'dashboard returned HTTP 500' while the README documents 'catalog backend not available' (404) for the same command — the two routing modes surface differe
