# MusterFlow Integration Report — 2026-09-10 (Round 4)

Fifth dogfood run; fourth with full CLI+server depth, first with the
ephemeral-bunker install leg. Scope: regression check of every P1 from
2026-09-01/09-07, plus the surfaces prior runs had not covered (all six
output formats, export/import, auth masking, webhooks, installability).

Environment: scratch data-dir `/tmp/dogfood-mf/data`, binary built at
`db8c90a` (45s), petstore3 upstream UP today (it was 500-dead for the three
prior runs — the flagship example is verifiable again).

## What was promised vs what happened

| Promise | Result |
|---|---|
| Connect spec → CLI subcommands | ✅ works (connect 1.4s, 19 endpoints, groups by tag) |
| Flagship petstore example | ✅ works today (empty upstream DB is NOT a bug — `curl` proves `[]`) |
| Create → read → find round-trip | ✅ works via per-property flags (`add-pet --id --name --status --photo-urls`) |
| 6 output formats (table/json/yaml/csv/jsonl/parquet) | ✅ all verified incl. real `PAR1` parquet; `--output-file` extension autodetect works |
| MCP: dynamic tools | ✅ ADD is live (19→20 without restart) — DF-015 fixed |
| MCP: tools/call | ✅ array responses fixed — DF-016 fixed |
| MCP: removal is dynamic | ❌ stale until restart (DF-029, 3rd consecutive run) |
| Workflows "chain API calls" | ❌ impossible — no builtins (DF-030) |
| Webhooks | ✅ POST /hooks/<name> → flow runs → JSON result back |
| Export/import JSONL | ✅ round-trip works (local mode); import takes positional path, not --input |
| Auth masking | ✅ `sk-d…-123` in list; real key only in that data-dir's config |
| Fresh-machine install | ❌ dead-ends at private engine; resolver exits 0 on failure (DF-031/032) |

## Regression scorecard vs 2026-09-07

| Prior finding | Status this run |
|---|---|
| DF-015 MCP frozen at start (dynamic-add false) | **FIXED** — connect while running → tools appear |
| DF-016 tools/call fails on arrays | **FIXED** — findPetsByStatus returns JSON array text |
| DF-017 array query params `[a b]` | **FIXED** — real `?tags=dogfood` sent |
| Flag asymmetry (flow run --name, catalog --query) | **PERSISTS** (now DF-034; import --input adds a new instance) |
| catalog search 500-vs-404 divergence | **PERSISTS** (DF-035) |
| MCP stale on disconnect (09-01) | **PERSISTS** (DF-029) |
| petstore3 upstream 500 | N/A — upstream healthy today |

## The working walkthrough (verified 2026-09-10)

```bash
go build -o musterflow ./cmd/musterflow/          # 45s, needs ./muster + go.work (resolve-engine.sh)
./musterflow --data-dir /tmp/mf connect https://petstore3.swagger.io/api/v3/openapi.json
./musterflow --data-dir /tmp/mf swagger-petstore-openapi-3-0 pet add-pet \
  --id 90210 --name DogfoodDog --status available --photo-urls 'https://example.com/d.png'
./musterflow --data-dir /tmp/mf swagger-petstore-openapi-3-0 pet get-pet-by-id 90210
./musterflow --data-dir /tmp/mf swagger-petstore-openapi-3-0 pet find-pets-by-status \
  --status available --output json        # json|yaml|csv|jsonl|parquet all work
./musterflow --data-dir /tmp/mf start      # dashboard+API+MCP+hooks on :9876

# MCP (add is live; delete is NOT — restart after disconnecting)
curl -X POST localhost:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"findPetsByStatus","arguments":{"status":"available"}}}'

# flows: only `trigger` and `print` exist — NO API-call builtins yet (DF-030)
./musterflow --data-dir /tmp/mf flow create hook-flow --webhook \
  --source 'def main():
  print("hook got: " + str(trigger))
main()'
curl -X POST localhost:9876/hooks/hook-flow -H 'Content-Type: application/json' -d '{"event":"dogfood","n":7}'
# → {"result":"hook got: {\"event\": \"dogfood\", \"n\": 7.0}"}

# export/import round-trip (use --no-dashboard for true dir isolation, DF-033)
./musterflow --data-dir /tmp/mf export --output /tmp/mf/apis.jsonl
./musterflow --data-dir /tmp/mf2 --no-dashboard import /tmp/mf/apis.jsonl   # positional path
```

## Friction log (chronological, this run)

1. README "From Source" step 1 unclonable on a clean machine (private
   engine) — no fallback documented. Bunker-verified. → DF-031
2. `resolve-engine.sh` prints `error: muster engine not found` and exits 0. → DF-032
3. Bunker agent (bare Debian) has no Go toolchain; README never mentions Go
   1.26 as a requirement — it only works if your distro ships something that new. → part of DF-031 fix
4. `import --input <path>` unknown flag; usage line says `import <path>`. → DF-034
5. `import` with `--data-dir` + running dashboard silently wrote the server's
   registry (success message, wrong destination). → DF-033
6. `flow run --name` unknown flag (create accepts --name). 3rd round. → DF-034
7. Disconnect leaves MCP tools registered until restart. 3rd round. → DF-029
8. `flow create` template is a bare comment — new users get zero DSL
   guidance and the top-level-`if trigger` pitfall is invisible. → DF-036

## Time-to-first-success

~3 min (build 45s + connect 1.4s + first API call), once the engine is
available. From a genuinely fresh machine: **blocked** (DF-031) — that is
the difference between the dev-inner-loop experience (good) and the
new-user experience (broken).

## Verdict

🟡 **PROMISING-BUT-ROUGH** — 5th consecutive run. The core product (spec →
CLI → MCP → webhooks) now works end-to-end and the round-3 P1s are
genuinely fixed; what remains are the two architectural claims the product
makes but doesn't deliver (live MCP removal, workflow API calls) and an
install path that only works for people who already have the private engine.
