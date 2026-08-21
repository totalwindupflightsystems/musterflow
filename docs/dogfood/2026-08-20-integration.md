# MusterFlow Dogfood — Integration Report (2026-08-20)

Second real-use dogfood run (first: 2026-08-10, DF-001..DF-014). This run
re-verified the previously-fixed paths and probed deeper surfaces (MCP
dynamics, error paths, catalog, output formats, auth isolation). All
findings below were reproduced live against a local echo API
(`/tmp/dogfood-mf-2026-08-20/echo-server.py`, port 18082) and petstore3
(`https://petstore3.swagger.io/api/v3/openapi.json`), with a scratch data
dir (`/tmp/dogfood-mf-2026-08-20/data`).

## Verified working (fixes from round 1 hold)

| Surface | Result |
|---|---|
| `connect` local file / URL | 88ms, clean output block, exit 0 |
| Generated subcommands with dashboard running | **DF-001 FIXED** — works (routed via dashboard) |
| Nested object + array request bodies | **DF-002 FIXED** — `meta` sent as object, `labels` as array |
| `--data-dir` isolation for config/auth | **DF-003 FIXED** — scratch config only; `~/.musterflow` untouched |
| `-o table/json/yaml` | **DF-005 FIXED** — all three render real output |
| `export`/`import` with dashboard running | **DF-006 FIXED** — round-trip verified |
| Unknown command/flag exit codes | **DF-007 FIXED** — exit 1, single error line |
| Help `Examples:` blocks | **DF-008/GAP-011 FIXED** — full `musterflow <api> <group> <op>` paths |
| Path params | **GAP-012 FIXED** — positional, shown in help |
| `flow run --payload` | **GAP-004 FIXED** — payload reaches Starlark `trigger` |
| `flow run` newline termination | **GAP-014 FIXED** |
| Webhook flows | work (`flow create --webhook`, POST `/hooks/<name>`) |
| MCP `tools/list` + `tools/call` on object responses | work; schemas are clean (no `"in"` keys) |
| Error paths (404/401/redirect) | HTTP error surfaced, exit 1 |
| Persistence | registry + flows survive dashboard restarts |
| Auth masking + remove | `secr…-123` masking; remove works |

## NEW findings (tasks DF-015..DF-024 on the board)

### DF-015 (P1) — "Dynamic" MCP registration is false
README: *"Dynamic — connect a new API while the server is running and tools
update without restart."* Reality: `tools/list` is frozen at server start.
`toolRegistry.Refresh()` runs once in `cmd/musterflow/main.go:206` and nothing
re-triggers it. Live: connected petstore while dashboard ran → `/api/apis`
shows 2 APIs and CLI subcommands work, but `tools/list` still returned the 7
echo tools; after a dashboard restart → 26 tools. The AI-agent integration
story (connect → agents immediately see tools) is broken.

### DF-016 (P1) — MCP tools/call fails on array responses
`findPetsByStatus` / `findPetsByTags` (petstore, README's own examples) →
`isError: decode response: json: cannot unmarshal array into Go value of type
map[string]interface {}`. Every list endpoint is unusable via MCP. The CLI
handles the same endpoints fine. Object responses (`getPetById`) work.

### DF-017 (P1) — Query array params are broken
`--tags a,b` → HTTP request `GET /widgets?limit=5&tags=[a b]` → 400 from any
real server. Repeated flags (`--tags a --tags b`) equally broken. Go `%v`
formatting of the slice leaks into the URL.

### DF-018 (P1) — Community catalog is a dead feature
`raw.githubusercontent.com/totalwindupflightsystems/musterflow-catalog/main/index.json`
→ 404. `catalog search` always says "No catalog entries found." (exit 0 —
silent; users can't tell empty from dead). `catalog push <name>` (the README
form) fails "get api via dashboard: not found" (only `<api-id>` works). Pull
404s.

### DF-019..DF-024 (P2)
- **DF-019** `catalog pull` errors print but exit **0** (scripts can't detect failure).
- **DF-020** README advertises CSV/JSONL/Parquet output; CLI rejects all three ("unsupported output format ... supported formats are table, json, yaml").
- **DF-021** table cells for objects/arrays render as Go `%v`: `map[id:1 name:Dogs]`, `[x]`.
- **DF-022** dead boilerplate flags `-n/--namespace`, `-w/--watch` on every generated leaf — silently no-op.
- **DF-023** no HTTP timeout on generated API calls (only the OAuth client has one) — a hung upstream hangs the CLI forever.
- **DF-024** name-vs-id lookup UX: `refresh <name>` and `catalog push <name>` fail cryptically; README `catalog push petstore` example is wrong.

## How to use it for real (updated quick start)

```bash
go build -o mf ./cmd/musterflow/
MF=./mf; D=/tmp/scratch-mf

# 1. connect (URL or local file) — always use --data-dir for scratch work
$MF --data-dir $D connect https://petstore3.swagger.io/api/v3/openapi.json

# 2. call endpoints — ops grouped by OpenAPI tag; path params positional;
#    query scalars are kebab flags; DO NOT use array query flags (DF-017)
$MF --data-dir $D swagger-petstore-openapi-3-0 pet find-pets-by-status --status available
$MF --data-dir $D swagger-petstore-openapi-3-0 pet get-pet-by-id 4 -o json

# 3. dashboard + MCP (all on :9876) — note: connect NEW APIs BEFORE start,
#    or restart after connecting, or MCP won't see them (DF-015)
$MF --data-dir $D start
curl http://127.0.0.1:9876/api/health          # NOT /health
curl -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"getPetById","arguments":{"petId":4}}}'

# 4. flows + webhooks
$MF --data-dir $D flow create greet --source 'def main():\n    print("hi " + str(trigger.get("name","?")))\nmain()'
$MF --data-dir $D flow run greet --payload '{"name":"x"}'
$MF --data-dir $D flow create hook --webhook --source 'def main():\n    print("got " + str(trigger))\nmain()'
curl -X POST http://127.0.0.1:9876/hooks/hook -d '{"a":1}'
```

## Errors you will hit and why

- `unsupported output format "csv"` — csv/jsonl/parquet are claimed but not implemented (DF-020).
- `cannot unmarshal array into Go value of type map...` — MCP tool on a list endpoint (DF-016).
- `tags=[a b]` in the server's request log → 400 — array query flags (DF-017).
- `No catalog entries found.` — always, the catalog backend 404s (DF-018).
- `refresh <name>` → `get api: not found` — use the ID (DF-024).

## Scratch artifacts

- Echo API + spec: `/tmp/dogfood-mf-2026-08-20/` (echo-server.py, echo-spec.json, data/).
- All probes above are re-runnable against those fixtures.
