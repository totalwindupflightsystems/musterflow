# MusterFlow Diagnostics — how it's built, errors hit, the right way

Explanatory trail from the 2026-08-10 dogfood run. Not raw logs — what the system
is, why it behaves the way it does, and the right way to work with it.

## Architecture (what's actually under the hood)

- **Go 1.26 module** `github.com/totalwindupflightsystems/musterflow`, binary at
  `cmd/musterflow/main.go`. Depends on the `github.com/wojons/muster` engine via a
  `replace` directive to an absolute local path (`/home/kara/muster`) — the engine
  repo is private, so fresh clones cannot build until that's resolved (board GAP-001).
- **Two execution modes, chosen at process start** (`cmd/musterflow/main.go:run()`):
  1. **Dashboard NOT running** → CLI opens the DuckDB registry read-write
     (`registry.Load()`), generates one cobra command per connected API
     (`loadAPISubcommands`, `internal/cli/api.go:31`), and everything works.
  2. **Dashboard running** (detected via `isPortInUse` on the configured port) →
     the CLI **skips loading the registry entirely** (BUG-001 fix: DuckDB
     read-only open fails with "Conflicting lock is held"), sets `dashboardBaseURL`,
     and routes `list/connect/disconnect/refresh/catalog/mcp/flow` through the
     dashboard HTTP API. **Generated API subcommands are NOT routed** — they're
     built from `registry.List()`, which is now empty → every API command becomes
     "unknown command". This is DF-001. The fix direction: generate subcommands
     from `GET /api/apis` (+ spec/schema endpoints) when in dashboard mode.
- **Dashboard** (`internal/dashboard/server.go:48-59`): routes registered on one
  mux — `/api/health`, `/api/apis[/<id>]`, `/api/flows[/<name>/run]`,
  `/api/catalog/search`, `/api/mcp/info`, `/mcp`, `/hooks/`, `/` (SPA fallback).
  The SPA fallback at `/` catches any unmatched GET — including `/health` — which
  is why the guide's `/health` "JSON health check" returns HTML (real route:
  `/api/health`).
- **MCP** (`internal/mcp` + `pkg/mcp/handlers` from muster): HTTP JSON-RPC 2.0 at
  `/mcp`. Tools are registered from connected APIs, named by **bare operationId**
  (no API prefix) — collision risk across multiple APIs, and the guide's
  `api__operation` naming is fiction. inputSchema properties carry extra
  `"in":"query"|"path"` keys — non-standard but tolerated by clients.
- **Flows** (`internal/workflow`): `.star` files in `<data-dir>/flows`, metadata
  sidecar `<name>.star.json`. Engine = Starlark via muster `pkg/dsl`. `trigger`
  is a global (None when no payload). **Top-level `if` statements don't compile**
  (Starlark language rule) — webhook handlers must wrap logic in functions.
  `/hooks/<name>` triggers ANY flow (webhook flag or not). `--payload` on
  `flow run` sets `trigger` (dashboard path wraps as `{"trigger": payload}`).
- **Config & credentials** (`internal/config`, `internal/auth`): config is YAML at
  `~/.musterflow/config.yaml` and credentials live **in the config file's `auth`
  section** (`internal/auth/manager.go:2`). `config.Load()` reads the default path
  and **ignores `--data-dir`** — the flag is pre-parsed for the registry only
  (BUG-004 fixed registry/flows, not config/auth). Consequence (DF-003): any
  `auth add` with a scratch `--data-dir` writes into the real home config.
- **Registry** (`internal/app`): DuckDB at `<data-dir>/musterflow.db`; connection
  IDs are hashes (`715e581e0d464caa`), API names derive from spec title
  (`swagger-petstore-openapi-3-0`).

## Errors encountered during the run (and the right way)

1. **"unknown command" for every API after `start`** — not a typo: DF-001. Right
   way: know that CLI tree and dashboard are mutually exclusive today; use curl/MCP
   for API calls while the dashboard is up.
2. **`--petId` unknown flag** — path params are positional args, query params are
   kebab-case flags. Right way: read `--help` per leaf; help text is boilerplate
   (wrong binary name `muster`, fake `--namespace`), so trust `Usage:` + `Flags:`
   sections, not `Examples:`.
3. **Nested object body fields → strings** — DF-002: `--meta '{"a":1}'` arrives as
   `"meta":"{\"a\":1}"`. Right way: don't use CLI for POSTs with nested objects
   until fixed; arrays need CSV (`--labels a,b`), not JSON.
4. **`export` → "registry not loaded"** — DF-006: only happens with dashboard up.
   Right way: stop dashboard, export, restart.
5. **Webhook guard compile error** — guide's `if trigger != None:` is invalid
   Starlark at top level. Right way: `flow create --webhook`, source like
   `def main():\n    print(str(trigger))\nmain()`.
6. **`/health` → HTML** — right way: `/api/health`.
7. **Test failure `TestLoadSpecData_HTTPError`** — the test dials fixed port 19999
   expecting refusal; any listener there (here: another project's leftover
   `memoryd-test` daemon) fails the suite. Right way: run `go test -short` in a
   clean environment; board DF-009 proposes a dead-address fix.

## What the run proved works (don't "fix" these)

- Spec fetch/parse → command generation is fast and correct for URL and file specs.
- MCP `tools/call` correctly maps query params, path params, and returns text
  content — verified against the echo API.
- Flows: create/list/run via CLI **and** dashboard API; `--payload`; webhooks with
  real payload delivery; `trigger` global.
- Persistence across restarts (DuckDB registry + flows dir).
- Export/import/disconnect/refresh work when the dashboard is stopped.
- Port auto-discovery (9877-9886) and `--dashboard-addr` override exist.

## Hygiene notes

- Never run `auth add` with `--data-dir` pointing anywhere but the real config
  until DF-003 is fixed — it writes to `~/.musterflow/config.yaml` regardless.
- The dogfood run's own pollution (a test apikey in the real config) was removed;
  the pre-existing `api/sk-test` entry was left untouched.
