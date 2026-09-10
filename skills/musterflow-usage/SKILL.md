---
name: musterflow-usage
description: >-
  How to actually use MusterFlow (OpenAPI spec -> CLI + MCP server + Starlark
  workflows). Real-use knowledge from dogfood rounds 2026-08-10 through
  2026-09-10 (DF-001..036): entry points, the verified walkthrough, the
  current pitfall list (stale MCP disconnects, no flow API builtins,
  fresh-install wall, routing/data-dir override) and working patterns.
  Load this before building on or testing musterflow.
version: 3.0.0
category: software-development
---

# MusterFlow Usage Skill

MusterFlow turns an OpenAPI spec into: (1) a CLI with subcommands per endpoint,
(2) an HTTP MCP server (JSON-RPC at `:9876/mcp`), (3) a Starlark workflow
engine (trigger+print only — no API-call builtins yet, DF-030).

## Entry points

- CLI binary: `cmd/musterflow/main.go` → `musterflow` (build: `go build -o musterflow ./cmd/musterflow/`)
- Dashboard + API + MCP + webhooks: `musterflow start` → all on `:9876` (9877-9886 fallback)
- Build prerequisite: the private `github.com/wojons/muster` engine, resolved
  by `bash scripts/resolve-engine.sh` (generates `go.work`; never edits
  `go.mod`). **Fresh machine without `../muster` = install blocked today**
  (DF-031); the resolver exits 0 on that failure (DF-032).
- Board: `.coding-hermes/board/tasks.jsonl` (JSONL v2.1)
- Foreman: `musterflow-foreman` cron (coding-hermes fleet)

## Verified walkthrough (re-verified 2026-09-10, binary db8c90a)

```bash
# connect (URL or local spec file) — everything from here on uses --data-dir
./musterflow --data-dir /tmp/mf connect https://petstore3.swagger.io/api/v3/openapi.json
./musterflow --data-dir /tmp/mf swagger-petstore-openapi-3-0 pet add-pet \
  --id 90210 --name Dogfood --status available --photo-urls 'https://x/d.png'
# path params POSITIONAL, body fields are kebab flags w/ inline allowed-values in --help;
# body arrays take CSV or JSON; raw --body must be JSON-encoded (replaces everything)
./musterflow --data-dir /tmp/mf swagger-petstore-openapi-3-0 pet find-pets-by-status \
  --status available --output json
# formats: table|json|yaml|csv|jsonl|parquet ALL work (DF-020 fixed);
# --output-file infers format from extension; INF logs go to stderr (pipes clean)

./musterflow --data-dir /tmp/mf start
curl -s localhost:9876/api/apis           # registry (NOTE: /api/health, not /health)
curl -X POST localhost:9876/mcp -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"findPetsByStatus","arguments":{"status":"available"}}}'

# flows work CLI-side AND via dashboard; webhook payload arrives as `trigger`
./musterflow --data-dir /tmp/mf flow create hook-flow --webhook \
  --source 'def main():
  print("hook got: " + str(trigger))
main()'
curl -X POST localhost:9876/hooks/hook-flow -H 'Content-Type: application/json' -d '{"x":1}'

# export/import (import takes a POSITIONAL path; --input does not exist)
./musterflow --data-dir /tmp/mf export --output /tmp/mf/apis.jsonl
./musterflow --data-dir /tmp/mf2 --no-dashboard import /tmp/mf/apis.jsonl
```

## Verified working (fixed across rounds 1-3 — do not re-file)

- CLI and dashboard coexist; CLI routes through the running server (DF-001).
- Nested bodies, per-property flags, CSV/JSON array body fields (DF-002+).
- `--data-dir` isolates config+auth+registry+flows in LOCAL mode (DF-003) —
  but see pitfall 2: a running dashboard silently overrides it.
- MCP tools/call handles ARRAY responses (DF-016 fixed 2026-09-10).
- MCP dynamic ADD: connect while running → tools appear without restart
  (DF-015 fixed; removal is still broken, pitfall 1).
- Array query params serialize correctly (DF-017 fixed).
- All six output formats incl. parquet (DF-020 fixed).
- `refresh`/`catalog push` accept name OR id (DF-024 fixed).
- Unknown commands / HTTP errors exit 1 with one clean line (DF-007).
- Export/import round-trip, auth masking (`sk-d…-123`), webhooks end-to-end,
  flows persist and survive restarts.

## Pitfalls (live-reproduced 2026-09-10 unless noted)

1. **MCP tool registry goes stale on DISCONNECT (DF-029, P1, 3rd run).**
   Remove an API while the server runs → /api/apis drops it but tools/list
   keeps serving its tools until restart. Dynamic ADD works; DELETE does not.
   Restart the server after disconnecting, or use a fresh data-dir.
2. **A running dashboard silently overrides `--data-dir` (DF-033, P2).**
   With the server on :9876, ALL CLI registry ops route to the server's
   registry — `import --data-dir <fresh-dir>` prints success but writes the
   server's registry. For true isolation add `--no-dashboard`.
3. **Workflows cannot call APIs (DF-030, P1).** The Starlark env has exactly
   two globals: `trigger` (payload dict, None when manual run) and `print`.
   `http_get` etc. are `undefined`. Flows = pure computation over the
   trigger payload + string output.
4. **No top-level `if trigger != None:`** — top-level statements cannot
   reference `trigger`; wrap logic in a `def` and call it.
5. **Flag-shape asymmetry (DF-034, P2, 3rd run):** `flow run` takes the name
   positionally only (`--name` unknown), `flow create` accepts both;
   `import` positional only. When in doubt, use positionals; check --help.
6. **Fresh-machine install dead-ends (DF-031/032, P1).** No wojons/muster
   access + no documented fallback + resolver exit-0 = you cannot build from
   README on a clean box. Dev boxes with a sibling `../muster` checkout work.
7. **catalog search error diverges by mode (DF-035, P2):** dashboard up →
   "dashboard returned HTTP 500"; local → "catalog backend not available
   (HTTP 404)". The backend repo does not exist yet; treat catalog as dead.
8. **`flow create` template is a bare comment (DF-036, P2)** — copy the
   webhook example above instead of relying on the generated template.
9. **Petstore3 upstream availability varies** — the flagship example 500'd
   upstream for three straight dogfooding weeks (2026-09-01..07), worked
   2026-09-10. Empty results ≠ bug; `curl` the upstream to compare.
10. **No HTTP timeout on API calls (DF-023, round 2)** — a hung upstream
    hangs the CLI.

## Testing pattern for agents

Always run with a scratch dir; add `--no-dashboard` when you care about
which registry you are writing:

```bash
export D="./musterflow --data-dir /tmp/mf-test --no-dashboard"
$D connect <spec-url-or-file> && $D list
```

## References

- `docs/dogfood/2026-09-10-integration.md` (round-4 report), `2026-08-20-integration.md`, `2026-08-10-integration.md`
- `docs/dogfood/diagnostics.md` (architecture + error trail)
- `docs/integration-guide.md`, `specs/cli.md`, `specs/dashboard.md`
