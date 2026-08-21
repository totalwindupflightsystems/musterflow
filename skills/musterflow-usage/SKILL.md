---
name: musterflow-usage
description: >-
  How to actually use MusterFlow (OpenAPI spec -> CLI + MCP server + Starlark
  workflows). Real-use knowledge from dogfood runs 2026-08-10 (DF-001..014)
  and 2026-08-20 (DF-015..024): entry points, the corrected walkthrough,
  known gotchas (MCP tools frozen at start, MCP array responses, query array
  flags, dead catalog, dead leaf flags) and the working patterns. Load this
  before building on or testing musterflow.
version: 2.0.0
category: software-development
---

# MusterFlow Usage Skill

MusterFlow turns an OpenAPI spec into: (1) a CLI with subcommands per endpoint,
(2) an HTTP MCP server (JSON-RPC at `:9876/mcp`), (3) a Starlark workflow engine.

## Entry points

- CLI binary: `cmd/musterflow/main.go` → `musterflow` (build: `go build -o musterflow ./cmd/musterflow/`)
- Dashboard + API + MCP + webhooks: `musterflow start` → all on `:9876`
- Board: `.coding-hermes/board/tasks.jsonl` (JSONL v2.1; tasks.md archived; board.db untracked)
- Foreman: `musterflow-foreman` cron (coding-hermes fleet)
- Engine: `github.com/wojons/muster` via local `replace` (private; GAP-001)

## The corrected quick start (re-verified 2026-08-20)

```bash
# connect (URL or local file), then call
./musterflow --data-dir <dir> connect https://petstore3.swagger.io/api/v3/openapi.json
./musterflow --data-dir <dir> <api> <group> <op> [flags]   # ops grouped by OpenAPI tag
# path params are POSITIONAL; query SCALAR params are kebab-case flags; check --help
# array body fields: CSV --labels a,b OR JSON --labels '["a","b"]' (both work since DF-002)
# array QUERY params: BROKEN (DF-017) — avoid

# dashboard + MCP
./musterflow --data-dir <dir> start     # then:
curl http://127.0.0.1:9876/api/health   # NOTE: /api/health, not /health
curl -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"getPetById","arguments":{"petId":4}}}'
# tools are bare operationIds; object responses work, ARRAY responses error (DF-016)

# flows (work via CLI AND dashboard API, dashboard up or down)
./musterflow flow create greet --source 'def main():
    print(str(trigger))
main()' --webhook
./musterflow flow run greet --payload '{"x":1}'
curl -X POST http://127.0.0.1:9876/hooks/greet -H 'Content-Type: application/json' -d '{"x":1}'
```

## Pitfalls (all reproduced live — see docs/dogfood/2026-08-20-integration.md)

1. **MCP tools are frozen at server start (DF-015, P1).** Connect new APIs
   BEFORE `musterflow start`, or restart the dashboard after connecting —
   otherwise tools/list and `musterflow mcp` never show them (CLI subcommands
   and /api/apis DO update live; only the MCP tool registry is stale).
2. **MCP tools/call fails on array-typed responses (DF-016, P1).** List
   endpoints (petstore `findPetsByStatus`) → `cannot unmarshal array into Go
   value of type map[string]interface {}`. Use the CLI for list endpoints.
3. **Array query flags are broken (DF-017, P1):** `--tags a,b` sends
   `?tags=[a b]` → HTTP 400. Avoid array query params via CLI until fixed.
4. **Community catalog is dead (DF-018, P1):** backend repo 404s; `catalog
   search` ALWAYS says "No catalog entries found." (exit 0); `push`/`pull`
   don't work. `catalog pull` errors also exit 0 (DF-019).
5. **`refresh`/`catalog push` take the API ID, not the name (DF-024):** the
   README example `catalog push petstore` fails; use the ID from `list`.
6. **Only table/json/yaml output exist (DF-020):** README's csv/jsonl/parquet
   are rejected with "unsupported output format".
7. **Table cells for objects/arrays render as Go %v** — `map[id:1 name:Dogs]`
   (DF-021). Use `-o json` for machine-readable output.
8. **`--namespace`/`--watch` leaf flags are dead no-ops (DF-022)** — ignore them.
9. **No HTTP timeout on API calls (DF-023):** a hung upstream hangs the CLI.
10. **Starlark: no top-level `if`** — webhook logic must go inside a function.
    Webhook-ness is the `flow create --webhook` flag, not the source text.
11. **`/health` returns HTML** — real health endpoint is `/api/health`.
12. **`flow run` output**: `{"result":"42"}` via API; CLI prints the value,
    newline-terminated (GAP-014).

## Verified working patterns (use these)

- `connect` → `list` → generated calls → `flow create/run` → dashboard → MCP →
  export/import all work; data and flows survive restarts.
- CLI and dashboard COEXIST since DF-001 — no need to stop the dashboard for
  CLI calls (round-1 pitfall #1 is fixed).
- `--data-dir` fully isolates config, auth, registry, and flows (DF-003 fixed)
  — safe to test with scratch dirs.
- Error paths: unknown commands exit 1 with one line (DF-007); HTTP 401/404
  surface as `Error: HTTP error: <code>` with exit 1; redirects followed.
- Petstore3 example: `swagger-petstore-openapi-3-0 pet find-pets-by-status --status available`
  (works from CLI; not from MCP until DF-016).

## References

- `docs/dogfood/2026-08-10-integration.md`, `docs/dogfood/2026-08-20-integration.md`
- `docs/dogfood/diagnostics.md` (architecture + error trail)
- `specs/cli.md`, `specs/dashboard.md`, `docs/integration-guide.md`
