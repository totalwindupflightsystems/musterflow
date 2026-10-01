# MusterFlow Dogfood Round 6 — 2026-10-01 (lane musterflow-docs)

**Promise (null hypothesis):** a user can connect any OpenAPI spec and get an
instant CLI, an HTTP MCP server for agents, and a Starlark workflow engine —
per README, in minutes, without reading source.

**Method:** real use at HEAD `5a842da`, binary built from source (cold build
~4 min incl. module downloads; the private-engine wall still gates every fresh
machine). Isolation: scratch `HOME`s (`/tmp/dogfood-mf-r6/home{,2,3}`), so the
real `~/.musterflow` was never touched. Workload: a scratch echo API
(OpenAPI 3.0 → local HTTP server on 127.0.0.1:18083) plus the documented
petstore3 flagship, driven CLI-side and MCP-side.

## Angle (why a 6th run adds signal)

Runs 1–5 swept the CLI surface, output formats, flows, and the install leg.
Round 6 took the surfaces nobody had touched: the **full MCP `initialize`
handshake** (prior runs curl'd `tools/list` cold), **multi-API MCP behavior**
(connect two APIs), **wire-level auth** (does a stored credential actually
reach the HTTP request? — header-echo server), **dynamic shell completion**,
and **re-verification of rows reopened by PM** (DF-022, DF-024) plus the
resolver fix (DF-032).

## What held up (verified live, exact commands in diagnostics)

- Documented quickstart works end to end: `connect` petstore3 (0.64s) →
  `find-pets-by-status` returns real data (upstream recovered; it 500'd for
  three weeks in Aug/Sep).
- All six output formats (DF-020 hold): table/json/yaml/csv/jsonl/parquet;
  parquet columns are TYPED now (id→DOUBLE, GAP-015's fix confirmed in DuckDB).
- MCP full handshake: `initialize` → protocolVersion 2024-11-05; **dynamic ADD
  confirmed again** (tools/list 24→29 with no restart); tools/call returns
  arrays + structuredContent.
- CLI CRUD cycle: create→list→get→404-negative→delete, exit codes 0/1
  correct, INF logs on stderr (stdout is pipe-clean JSON).
- **DF-022 FIXED**: dead `--namespace/--watch` flags are gone from generated
  leaf help (grep count 0).
- **DF-024 FIXED**: `refresh` accepts name AND id; `✓ Refreshed dogfood-echo-api`.
- **DF-028 hold**: `flow create` writes the `.star` file it tells you to edit.
- **DF-032 FIXED**: `resolve-engine.sh` exits 1 on missing engine, 0 with a
  clean go.work on success (verified both paths in a scratch clone).
- Dynamic completion holds: `__complete` lists connected APIs instantly,
  including one connected seconds earlier.
- `config show/set`, `auth add/list/remove` (masking display-only), export
  (JSONL), flow contract (`print(42)` / `trigger` through CLI and dashboard).
- Cold/warm perf: connect 26ms cold / ~10ms warm; generated call 19ms warm
  local-mode, 12ms via dashboard routing. Nothing slow enough to file.

## What broke (the round's findings — DF-039..043 on the board)

1. **DF-039 (P1) MCP tool-name collisions.** Tool names are bare operationIds.
   Connecting a second API with common operationIds (`listWidgets`…) puts
   duplicate names in tools/list AND last-connect-wins dispatch silently
   redirects the first API's tool to the second API's base URL. Compounds
   DF-029 after disconnect. CLI namespaces by API name; MCP doesn't.
2. **DF-040 (P1) Auth is split-brained.** `auth add` stores plaintext in
   `config.yaml`; the generated `--auth` flag resolves from the system
   keychain (engine service `muster-cli`). Wire-proven with a header-echo:
   musterflow-stored credentials are NEVER attached to any request (zero
   auth headers, stored under name or id, CLI and MCP). The engine's own CLI
   writing the keychain bridges instantly — so the fix is store alignment,
   not new plumbing. The `--auth` error hint references `--name/--value`
   flags that don't exist in musterflow's CLI.
3. **DF-041 (P2)** credential-bearing `config.yaml` written 0644 (plaintext).
4. **DF-042 (P2)** documented escape hatch `--no-dashboard` dead-ends with a
   raw DuckDB lock error while the server runs.
5. **DF-043 (P2)** SKIPPED-install-bunker: no bunkerd credential obtainable
   (all bunker hosts refuse ssh keys; CLI has zero registered servers).
6. **Re-proven, not re-filed:** DF-029 (4th run — disconnect leaves 29 tools),
   DF-030 (flows still have zero API builtins), DF-036 (template still bare).

## The auth investigation (how the P1 was found — for the next agent)

`auth get` printed what looked like a masked key → suspected "masked at
rest" P0 → file bytes proved RAW storage → the earlier "mask" was the
terminal's own output redaction mirage (verify displayed secrets with `od -c`
/ length checks before believing them). Then the wire test: a local
header-echo API received ZERO auth headers with credentials stored under
every plausible key. Source reading showed `ExecuteOptions.AuthManager/APIID`
(the auto-resolve path) has no non-test constructor — dead code on the live
path — and the engine resolves `--auth` via `muster/pkg/auth.ResolveStored`
→ system keychain. Cross-tool proof: `muster auth add --name wirecred …`
then `musterflow <api> <op> --auth wirecred` sends the header. Lesson: in
this repo, verify auth features at the WIRE, not the CLI exit code — every
check can be green while no credential moves.

## Time-to-first-success

~2 min from clone on a dev box with a sibling engine (build ~40s warm,
connect 0.6s). From a truly fresh machine: **still blocked** at the private
engine (DF-031) — the resolver now fails honestly (DF-032 fixed), but there
is no documented fallback.

## Verdict

🟡 **PROMISING-BUT-ROUGH** (round 6; 6th consecutive). The CLI product is
genuinely good and most of round-2/3's P1 fixes hold at HEAD. The AI-agent
story (the second headline) is where the value leaks: tools collide across
APIs, deleted APIs keep serving tools, and credentials never reach the wire.
Fix DF-039 + DF-029 + DF-040 and this flips to SHIPPABLE.
