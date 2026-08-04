# MusterFlow E2E Report — Tick 63 (2026-08-04)

## Verdict: 27/27 PASS + 8/8 corrected harness checks — ZERO new gaps

E2E-001 recurring cycle (window tick 63-68, ran at first tick of window). Foreman-direct battery (CLI/API project, no browser surface — per e2e-testing-tick-template CLI-only variant). Fresh binary built from HEAD 3208a19, isolated --data-dir, dashboard on CONFIG port :9876 (routing requirement — scratch ports make CLI run local-only).

## Battery

| # | Check | Result | Evidence |
|---|-------|--------|----------|
| 1 | Dashboard start on :9876 with isolated --data-dir | ✅ PASS | health 200 after 2s |
| 2 | BUG-002: flow create routes via dashboard API | ✅ PASS | rc=0, `✓ Created flow "e2e-verify"` |
| 3 | BUG-002: flow list routes via dashboard API | ✅ PASS | flow visible in CLI list AND /api/flows (negative control) |
| 4 | BUG-001: flow run byte-exact `42\ntrigger=none` | ✅ PASS | stdout matches byte-for-byte |
| 5 | BUG-001: stderr empty (no DuckDB lock warning) | ✅ PASS | stderr empty while dashboard holds DB lock |
| 6 | E2E38-001: --source round-trip to .star file | ✅ PASS | flows/e2e-verify.star content == canonical source |
| 7 | E2E38-002: --description sidecar persistence | ✅ PASS | sidecar: `{"description":"e2e desc","webhook":false}` |
| 8 | E2E38-003: no webhook_url on non-webhook flow | ✅ PASS | API entry has description/source, no webhook_url key |
| 9 | Webhook flow gets /hooks/<name> URL | ✅ PASS | hook-flow → `http://localhost:9876/hooks/hook-flow` |
| 10 | BUG-003: flows stored under --data-dir/flows | ✅ PASS | $DATADIR/flows/e2e-verify.star + hook-flow.star exist |
| 11 | BUG-004: ~/.musterflow/flows untouched | ✅ PASS | 3 -> 3 .star files |
| 12 | BUG-004: home db mtime unchanged | ✅ PASS | mtime identical pre/post |
| 13 | BUG-004: registry DB isolated under data dir | ✅ PASS | musterflow.db present in $DATADIR |
| 14 | Starlark real exec: print(6*7) -> 42 | ✅ PASS | computed output, not stub |
| 15 | Starlark trigger global | ✅ PASS | trigger=None index errors correctly ("unhandled index operation NoneType") — correct semantics |
| 16 | API: /api/health GET | ✅ PASS | 200 |
| 17 | API: /api/apis GET | ✅ PASS | 200 |
| 18 | API: /api/apis/<id> GET unknown | ✅ PASS | 404 |
| 19 | API: /api/flows GET | ✅ PASS | 200 |
| 20 | API: POST /api/flows/<name>/run | ✅ PASS | 200 + `{"result":"42\ntrigger=none"}` byte-exact |
| 21 | API: POST /api/flows/<unknown>/run | ✅ PASS | 404 |
| 22 | API: /api/catalog/search | ✅ PASS | 200 |
| 23 | API: /api/mcp/info | ✅ PASS | 200 |
| 24 | API: /mcp POST (valid JSON-RPC tools/list) | ✅ PASS | 200 + `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}` (matches dashboard_test.go:319 contract) |
| 25 | API: /mcp POST (empty body) | ✅ PASS | 200 + `{"code":-32700,"message":"Parse error"}` (per contract) |
| 26 | API: /hooks/<flow> GET | ✅ PASS | 200 |
| 27 | API: / (index) | ✅ PASS | 200 |
| 28 | Live webhook trigger POST /hooks/t63-hook | ✅ PASS | 200 + `{"result":"42\ntrigger=none"}` — live trigger executed |
| 29 | /hooks contract (307 -> /hooks/ -> 400 missing name) | ✅ PASS | designed contract, per tick-58 note |
| 30 | POST /hooks/<unknown> | ✅ PASS | 404 |
| 31 | /api/<unknown> SPA fallback HTML 200 | ✅ PASS | index fallback (expected; real 404 via /api/apis/<unknown>) |
| 32 | Routing negative control: CLI list + /api/flows both show flow | ✅ PASS | cli=1 api=1 |

## Corrected harness checks (5 FAILs re-verified — all script artifacts, 0 app bugs)

| # | Initial FAIL | Root cause | Re-verify |
|---|--------------|------------|-----------|
| 1 | BUG-002 flow list | jq `.[]` on wrapped `{"flows":[...]}` — needs `.flows[]` | ✅ PASS (cli + api count=1) |
| 2 | E2E38-003 no webhook_url | same wrapped-JSON jq error | ✅ PASS (key absent per-entry) |
| 3 | Webhook flow URL | same wrapped-JSON jq error | ✅ PASS (http://localhost:9876/hooks/hook-flow) |
| 4 | Live webhook trigger 404 | flow t63-hook not created before POST — 404 = flow missing, not bug | ✅ PASS (after create, POST -> 200 + result) |
| 5 | Routing negative control | same wrapped-JSON jq error | ✅ PASS (cli=1 api=1) |

## Notes

- All 7 regression fixtures (BUG-001..004, E2E38-001..003) re-verified PASS with zero drift since tick 58.
- Battery harness corrections (not app bugs): /api/flows returns wrapped `{"flows":[...]}` — jq must use `.flows[]`; webhook trigger needs the flow created first.
- Full gates same tick: build/vet PASS, 11/11 pkgs green (serial -p 1, GOMAXPROCS=2, first run clean), golangci 0 issues, gofmt clean, gitreins guard PASS (5/5 tier1), Hilo 330/49 fresh.
- CI: ci-workflow GREEN (30917994804). docker FAIL (30917992041) = DOCKER-GHCR-001 blocked-human unchanged.
- 0 new gaps filed. Next E2E window ~tick 68-73.
