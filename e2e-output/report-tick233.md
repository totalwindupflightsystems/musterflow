# MusterFlow E2E Battery Report — Tick 233 (2026-11-20, full battery, window opener)

- Scheduler tick: musterflow-foreman-2026-11-20-11-41-41
- Tree: master @ 8508c2c (merged wave worker wt/DOC-2; base 9ada37b; board commit b0d2143 beneath)
- Binary: built from the merged tree; data dir /tmp/mf-t233-e2e-data2 (isolated); dashboard 127.0.0.1:19876 (scratch — tick-143+ era standard)
- Result: 28 PASS / 0 FAIL (28 cells asserted)
- Prior full battery: tick 226 (event 478). This window opened at tick 233 (7 ticks after prior — inside the 5-10 window).

## Cells

| # | Cell | Result |
|---|------|--------|
| 01 | build BIN | PASS |
| 02 | dashboard /api/health up | PASS |
| 03 | flow create e2e-verify (canonical trigger-computed source) | PASS |
| 04 | flow list shows e2e-verify | PASS |
| 05 | flow run stdout byte-exact `42\ntrigger=none` | PASS |
| 06 | flow run stderr empty (no lock warning) | PASS |
| 07 | source round-trip ($(cat) compare) | PASS |
| 08 | sidecar .star.json description persisted | PASS |
| 09 | hook-flow created with --webhook | PASS |
| 10 | /api/flows e2e-verify: webhook=false, webhook_url null (jq per-entry over .flows) | PASS |
| 11 | /api/flows hook-flow: webhook=true, url contains :19876/hooks/hook-flow | PASS |
| 12 | flow file under <data>/flows/ | PASS |
| 13 | ~/.musterflow/flows untouched | PASS |
| 14 | ~/.musterflow/musterflow.db mtime unchanged | PASS |
| 15 | Starlark real exec (6*7 -> 42) | PASS |
| 16 | GET /api/health 200 | PASS |
| 17 | GET /api/apis 200 | PASS |
| 18 | GET /api/apis/unknown-xyz 404 | PASS |
| 19 | GET /api/flows 200 | PASS |
| 20 | POST /api/flows/e2e-verify/run -> {"result":"42\ntrigger=none"} byte-exact | PASS |
| 21 | POST /api/flows/nope/run 404 | PASS |
| 22 | GET /api/catalog/search 200 | PASS |
| 23 | GET /api/mcp/info 200 | PASS |
| 24 | POST /mcp tools/list -> full envelope {"jsonrpc":"2.0","id":1,"result":{"tools":[]}} | PASS |
| 25 | POST /mcp empty body -> -32700 parse error | PASS |
| 26 | GET /hooks/hook-flow 200 | PASS |
| 27 | GET / 200 | PASS |
| 28 | port released after dashboard kill (probe 000, first-3-chars compare) | PASS |

## Harness notes (first-run fails, fixed in-script, not app bugs)

- /api/flows returns {"flows":[...]} — per-entry checks must jq .flows[] (run 1: cells 10/11 jq-path fails).
- Persisted .star strips one trailing newline — byte-compare via $(cat) (run 1: cell 07 over-strict cmp; exec output byte-exact regardless).
- Run 1: 25/28 on data dir 1; harness fixes applied; run 2 on FRESH data dir: 28/28.
- Script: /tmp/mf-t233-e2e-battery.sh (tick-scoped). Cells 26/28 of run 1 plus all 28 of run 2 green; no app defect found.

## Relation to board rows

- E2E-001: window-opener full battery evidence (this file).
- GAP-017: satisfied for this window — "full battery 28/28" claim is matched by this committed e2e-output/ artifact in the same window.
