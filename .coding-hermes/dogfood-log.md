# MusterFlow Dogfood Log

## 2026-08-10 — Verdict: 🟡 PROMISING-BUT-ROUGH

- **Promise:** Connect an OpenAPI spec URL → get a CLI with subcommands for every endpoint, an HTTP MCP server for AI agents, and a Starlark workflow engine.
- **Reality:** The happy path genuinely works (connect 0.46s, real API calls, MCP tools/call, flows, webhooks, persistence). But the documented combined workflow breaks: starting the dashboard removes every generated API subcommand from the CLI (DF-001, P0), nested-object request bodies are sent as strings (DF-002, P1), and `--data-dir` is ignored by config/auth — a scratch-dir test run wrote a credential into the real `~/.musterflow/config.yaml` (DF-003, P1). The flagship integration guide has 5 wrong examples (DF-004).
- **Time-to-first-success:** ~1 min (build 40s + `connect petstore` 0.46s + first API call).
- **Top 3 findings:**
  1. DF-001 (P0): API subcommands vanish while dashboard runs.
  2. DF-002 (P1): nested object body fields serialized as JSON strings.
  3. DF-003 (P1): `--data-dir` not honored for config/auth → real config pollution.
- **Tasks written:** DF-001..DF-009 (9 tasks) in `.coding-hermes/board/tasks.jsonl`.
- **Artifacts:** `docs/dogfood/2026-08-10-integration.md`, `docs/dogfood/diagnostics.md`, `skills/musterflow-usage/SKILL.md`.
- **Foreman:** cooldown 7200s — no wake needed; will pick up DF-001..009 on next ticks.
- **Environment notes:** full `go test -short` currently fails 1 test (TestLoadSpecData_HTTPError) because port 19999 is occupied by `memoryd-test` (another project's leftover daemon) — see DF-009. Scratch run used `/tmp/dogfood-musterflow` (binary, data dirs, echo API). The real `~/.musterflow/config.yaml` was touched by the DF-003 bug during the run; the dogfood credential was removed afterwards (pre-existing `api/sk-test` entry left untouched).

## 2026-08-20 — Verdict: 🟡 PROMISING-BUT-ROUGH

- **Promise:** Connect an OpenAPI spec URL → get a CLI with subcommands for every endpoint, an HTTP MCP server for AI agents, and a Starlark workflow engine.
- **Reality:** The core promise now genuinely holds end-to-end — all round-1 P0/P1 fixes verified live (DF-001 CLI+dashboard coexistence, DF-002 nested bodies, DF-003 data-dir isolation, DF-006 export, DF-007 exit codes, GAP-004 payload, GAP-011/012 help+path params, GAP-014 newline). But the AI-agent surface (the second headline feature) has two P1 breaks: MCP tools are frozen at server start ("dynamic" claim false, DF-015) and tools/call fails on every array-returning endpoint (DF-016). Query array params are broken (DF-017), and the community catalog is a dead feature pointing at a 404 repo (DF-018).
- **Time-to-first-success:** ~2 min (build 40s + connect 88ms + first API call).
- **Top 3 findings:**
  1. DF-015 (P1): MCP "dynamic" registration false — tools/list frozen at start; new API needs dashboard restart (7 tools → 26 after restart).
  2. DF-016 (P1): MCP tools/call fails on array responses — README's own petstore example errors; CLI handles it fine.
  3. DF-017 (P1): query array params serialize as `[a b]` → HTTP 400.
- **Tasks written:** DF-015..DF-024 (10 tasks) in `.coding-hermes/board/tasks.jsonl` + event 228.
- **Artifacts:** `docs/dogfood/2026-08-20-integration.md`, `docs/dogfood/diagnostics.md` (round-2 section), `skills/musterflow-usage/SKILL.md` (v2.0, refreshed).
- **Foreman:** cooldown 21600s ≥ 14400s → woken via scheduler PUT CooldownS=900 (Enabled=true, not touched otherwise).
- **Environment notes:** scratch run in `/tmp/dogfood-mf-2026-08-20` (binary, data dirs, echo API on :18082). No pollution of `~/.musterflow` (DF-003 fix verified). Dashboard was stopped/restarted several times during the run; final state: stopped.
2026-09-01 | PROMISING-BUT-ROUGH | 12s t2fs | friction 7 | 5 findings
