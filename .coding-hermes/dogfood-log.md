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
