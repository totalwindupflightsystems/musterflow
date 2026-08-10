# MusterFlow Integration Report — 2026-08-10

Real-use dogfood run: built from source, connected two real APIs (petstore3 over the
internet + a local echo API), called endpoints, ran the dashboard, MCP, flows,
webhooks, auth, export/import. Everything below was executed, not simulated.

## What works (verified live)

| Surface | Result |
|---|---|
| Build | `go build -o musterflow ./cmd/musterflow/` — clean, ~40s (needs `/home/kara/muster` engine via `replace`; see AGENTS.md prereq note) |
| `connect <url-or-file>` | 0.46s for petstore3; local spec file also works. Output: `✓ Connected: <name> / ID / Version / Endpoints / Base URL` |
| Generated CLI calls | `musterflow <api> <group> <op> [flags]` — real HTTP calls succeed (petstore `pet find-pets-by-status --status available`, `pet get-pet-by-id 1`; echo API `widgets get-widget abc`) |
| Dashboard | `musterflow start` → :9876. `/api/health` → `{"status":"ok","connected_apis":N}` (note: NOT `/health`), `/api/apis`, `/api/apis/<id>`, `POST /api/apis`, `/api/flows`, `POST /api/flows/<name>/run`, `/api/catalog/search`, `/api/mcp/info` |
| MCP | JSON-RPC 2.0 at `/mcp`: `tools/list` + `tools/call` work end-to-end (query params → `?limit=5`, path params → `/widgets/abc`) |
| Flows | `flow create <name> --source '...' --description ...`, `flow list`, `flow run <name>` → `42`; `flow run <name> --payload '{"x":1}'` delivers `trigger["x"]`; webhook flows via `flow create --webhook` → `POST /hooks/<name>` delivers payload to `trigger` (inside a function) |
| Persistence | DuckDB registry + flows survive dashboard restart |
| Other commands | `list`, `refresh`, `disconnect`, `export`/`import` (dashboard **stopped**), `auth add/list` (dashboard **stopped** — see warning below), `config show` (⚠), `catalog search` (empty catalog), `completion`, `transform`, `mcp` |

## The right way to use it (corrected walkthrough)

```bash
# 1. Build (requires the muster engine checkout — see AGENTS.md)
go build -o musterflow ./cmd/musterflow/

# 2. Connect — from URL or local file
./musterflow --data-dir ~/.musterflow connect https://petstore3.swagger.io/api/v3/openapi.json

# 3. Call endpoints — commands are grouped by OpenAPI tag, ops are kebab-case leaves
./musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available
./musterflow swagger-petstore-openapi-3-0 pet get-pet-by-id 1          # path params are POSITIONAL
./musterflow <api> <group> <op> --help                                 # check real flags first

# 4. Start the dashboard + MCP server (NOTE: while it runs, CLI API subcommands
#    disappear — known bug DF-001. Plan CLI calls before `start`, or use curl/MCP.)
./musterflow start          # :9876 — dashboard, /api/*, /mcp, /hooks

# 5. MCP from any client
curl -X POST http://localhost:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
# tools/call with {"name":"<operationId>","arguments":{...}} — names are BARE operationIds

# 6. Flows
./musterflow flow create greet --source 'print("hello " + str(trigger))' --webhook
./musterflow flow run greet --payload '{"name":"world"}'
curl -X POST http://localhost:9876/hooks/greet -H 'Content-Type: application/json' -d '{"name":"world"}'

# 7. Auth — WARNING: --data-dir is ignored for auth/config (DF-003); use the real
#    config or expect credentials in ~/.musterflow/config.yaml
./musterflow auth add <api-id> --type apikey --key sk-...
```

## Errors hit and their fixes (or workarounds)

| Error | Cause | Workaround / fix |
|---|---|---|
| `Error: unknown command "swagger-petstore-openapi-3-0"` after `start` | DF-001: registry skipped when dashboard running | Stop dashboard for CLI calls, or use `curl`/MCP; fix on board |
| `Error: unknown flag: --petId` | Path params are positional, not flags | `get-pet-by-id 1` (positional), kebab-case for query flags |
| `Error: unknown flag: --limit` | Docs example; that flag/command doesn't exist for petstore3 | Check `--help`; use `--status available` |
| `invalid argument "[...]" for "--labels" flag: parse error ... bare " in non-quoted-field` | Array body fields use pflag CSV syntax | `--labels a,b` (no JSON brackets) |
| `HTTP 400 unable to convert input to Pet` (add-pet with `--category '{...}'`) | DF-002: nested objects sent as strings | Use a client that sends real JSON until fixed |
| `Error: registry not loaded` on `export` while dashboard up | DF-006: no dashboard route for export | Stop dashboard before export/import |
| `compile flow X: if statement not within a function` | Starlark: no top-level `if`; guide's webhook example is wrong | Wrap in `def main(): ...` / use `flow create --webhook` flag |
| `GET /health` returns HTML | Real route is `/api/health` | Use `/api/health` |

## Integration verdict

MusterFlow delivers real value on the happy path — spec→CLI in under a second, MCP
tools that genuinely work, flows/webhooks that run. The blockers are integration
hygiene: the dashboard kills the CLI tree (P0), nested bodies break POSTs (P1),
`--data-dir` leaks into the real config (P1), and the guide lies in 5 places (P1).
Fix those and this is a genuinely useful "OpenAPI → tools" glue. Verdict:
🟡 PROMISING-BUT-ROUGH. Tasks DF-001..DF-009 track everything.
