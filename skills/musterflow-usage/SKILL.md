---
name: musterflow-usage
description: >-
  How to actually use MusterFlow (OpenAPI spec -> CLI + MCP server + Starlark
  workflows). Real-use knowledge from the 2026-08-10 dogfood run: entry points,
  the corrected walkthrough, known gotchas (dashboard kills CLI subcommands,
  nested body fields, --data-dir leak, Starlark top-level if), and the working
  patterns. Load this before building on or testing musterflow.
version: 1.0.0
category: software-development
---

# MusterFlow Usage Skill

MusterFlow turns an OpenAPI spec into: (1) a CLI with subcommands per endpoint,
(2) an HTTP MCP server (JSON-RPC at `:9876/mcp`), (3) a Starlark workflow engine.

## Entry points

- CLI binary: `cmd/musterflow/main.go` → `musterflow` (build: `go build -o musterflow ./cmd/musterflow/`)
- Dashboard + API + MCP + webhooks: `musterflow start` → all on `:9876`
- Board: `.coding-hermes/board/tasks.jsonl` (JSONL v2.1; tasks.md is archived)
- Foreman: `musterflow-foreman` cron (coding-hermes fleet)

## The corrected quick start (verified 2026-08-10)

```bash
# connect (URL or local file), then call
./musterflow --data-dir <dir> connect https://petstore3.swagger.io/api/v3/openapi.json
./musterflow --data-dir <dir> <api> <group> <op> [flags]   # ops grouped by OpenAPI tag
# path params are POSITIONAL; query params are kebab-case flags; check --help
# arrays in bodies: CSV syntax --labels a,b (NOT JSON); objects: --meta '{"a":1}' (BROKEN, see below)

# dashboard + MCP
./musterflow --data-dir <dir> start     # then:
curl http://127.0.0.1:9876/api/health   # NOTE: /api/health, not /health
curl -X POST http://127.0.0.1:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
# tools/call: {"name":"<operationId>","arguments":{...}} — bare operationIds, no API prefix

# flows (work via CLI AND dashboard API, dashboard up or down)
./musterflow flow create greet --source 'def main():
    print(str(trigger))
main()' --webhook
./musterflow flow run greet --payload '{"x":1}'
curl -X POST http://127.0.0.1:9876/hooks/greet -H 'Content-Type: application/json' -d '{"x":1}'
```

## Pitfalls (all reproduced live — see docs/dogfood/)

1. **Dashboard running ⇒ CLI API subcommands VANISH** (DF-001, P0). Plan CLI API
   calls before `musterflow start`, or use curl/MCP while it's up. `list`,
   `flow *`, `connect`, `refresh` still work via dashboard routing.
2. **Nested object body fields are sent as JSON strings** (DF-002, P1). Don't use
   the CLI for POSTs with object-typed body fields; array fields want CSV.
3. **`--data-dir` is ignored by config/auth** (DF-003, P1): `auth add` writes to
   the REAL `~/.musterflow/config.yaml` even with `--data-dir /tmp/x`. Never test
   auth with a scratch dir until fixed; clean up `~/.musterflow/config.yaml` after.
4. **Starlark: no top-level `if`** — the integration guide's webhook example
   (`if trigger != None:` at top level) does NOT compile. Wrap in a function.
   Webhook-ness is the `flow create --webhook` flag, not the source text.
5. **`export`/`import` fail with "registry not loaded" while dashboard runs**
   (DF-006). Stop the dashboard first.
6. **`/health` returns HTML** — real health endpoint is `/api/health`.
7. **`flow run` output contract**: `{"result":"42"}` (not `{"output":...}`).
8. **Unknown subcommands exit 0** — check exit codes in scripts (DF-007).
9. **Test fragility**: `TestLoadSpecData_HTTPError` fails if anything listens on
   port 19999 (DF-009) — unrelated to your change if you see it.
10. **Help Examples are boilerplate** (`muster ... --namespace production`) —
    trust `Usage:`/`Flags:`, not `Examples:` (DF-008).

## Verified working patterns (use these)

- `connect` → `list` → generated calls → `flow create/run` all work with the
  dashboard stopped; MCP tools/call works with it running; flows + webhooks work
  in both modes; data survives restarts.
- MCP tool naming: bare operationId; same name across two APIs collides (last wins).
- Petstore3 example: `swagger-petstore-openapi-3-0 pet find-pets-by-status --status available`.

## References

- `docs/dogfood/2026-08-10-integration.md` — full integration report + error table
- `docs/dogfood/diagnostics.md` — architecture and root causes
- `docs/integration-guide.md` — upstream doc (some examples are wrong; see DF-004)
- `specs/cli.md`, `specs/dashboard.md` — command/API specs
