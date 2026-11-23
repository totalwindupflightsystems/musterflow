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
- Port auto-discovery (range `port` … `port+9` — 9877-9885 when 9876 is taken) and `--dashboard-addr` override exist.

## Hygiene notes

- Never run `auth add` with `--data-dir` pointing anywhere but the real config
  until DF-003 is fixed — it writes to `~/.musterflow/config.yaml` regardless.
- The dogfood run's own pollution (a test apikey in the real config) was removed;
  the pre-existing `api/sk-test` entry was left untouched.

---

# Round 2 (2026-08-20) — what changed, new internals, new errors

## Status of round-1 architecture notes (read these as UPDATED)

Round 1's "two execution modes" split is **largely fixed**: generated API
subcommands now survive `musterflow start` (DF-001, fixed via dashboard
routing of the command tree), `--data-dir` threads through config/auth
(DF-003), body serialization handles nested objects/arrays (DF-002), export
routes through the dashboard (DF-006), and unknown commands exit 1 (DF-007).
The old "right way" workarounds (stop dashboard before CLI calls, never POST
objects) no longer apply. What remains structurally true:

- CLI in dashboard mode still routes most commands over HTTP to the dashboard;
  `--no-dashboard` forces local mode; `--dashboard-addr` overrides detection.
- MCP tool names are bare operationIds (collision risk across APIs stands).
- Top-level Starlark `if` still doesn't compile; webhook logic goes in functions.
- `/health` still returns the SPA (real route: `/api/health`).

## How the MCP tool registry actually works (round-2 finding DF-015)

`internal/mcp/tools.go` `ToolRegistry` holds an in-memory `tools []handlers.Tool`
+ `toolConfigs` map. `Refresh()` re-fetches EVERY connected API's spec and
rebuilds the list — but it is invoked from exactly ONE call site:
`cmd/musterflow/main.go:206`, at server startup. Nothing else calls it:
- `POST /api/apis` (connect) writes the connection to DuckDB and returns — no
  registry refresh.
- `/api/apis/<id>/refresh` refreshes the spec row, not the tool registry.
- `musterflow refresh` is a CLI-side spec refresh.

Consequence: tools/list (and `musterflow mcp`) is a snapshot of the APIs that
existed when the server started. New APIs appear only after a restart. This
is why the README's "dynamic" claim fails even though /api/apis and the CLI
command tree DO update live — the tool registry is the one path that isn't
wired to the connect event.

## New internals learned this run

- **MCP response decoding** (DF-016): the tools/call wrapper decodes the
  upstream JSON into `map[string]interface{}`; array top-level responses
  (`findPetsByStatus` returns `[...]`) fail with `cannot unmarshal array into
  Go value of type map[string]interface {}` → returned as isError. The CLI's
  own call path decodes into `any` and renders arrays fine.
- **Query serialization** (DF-017): generated flags for `type: array` query
  params produce `fmt.Sprintf("%v", slice)` → `?tags=[a b]`. Body arrays were
  fixed in round 1 (DF-002); query arrays were missed.
- **Catalog** (DF-018): `internal/catalog/client.go:13` hardcodes
  `raw.githubusercontent.com/totalwindupflightsystems/musterflow-catalog/main`
  as the index. That repo 404s (absent or private). Search failures collapse
  into "No catalog entries found." — there is no error path that distinguishes
  "empty catalog" from "backend unreachable". `push` prints a PR invitation to
  the same 404 repo. The dashboard proxies search via `/api/catalog/search`.
- **Lookups are ID-only**: dashboard routes `/api/apis/<id>`, so `refresh` and
  `catalog push` accept only connection IDs; names fail with "not found".
- **No API-call timeout**: the generated-call HTTP client (engine side) has no
  Timeout; only `internal/cli/oauth.go` configures one. A hung upstream hangs
  the CLI indefinitely (DF-023).
- **Leaf flag boilerplate**: every generated leaf gets `-n/--namespace` and
  `-w/--watch` from the engine generator; both silently no-op (DF-022).

## Errors hit this run and the right way

1. `GET /widgets?tags=[a b]` → 400 — array query flag. Right way: avoid array
   query flags; use repeated flags only after DF-017 lands, or call via MCP/curl.
2. MCP `tools/call` on a list endpoint → unmarshal error. Right way: use the
   CLI for list endpoints until DF-016 lands.
3. `catalog search github` → "No catalog entries found." — always true today,
   the backend is absent. Right way: treat catalog as not-yet-available (DF-018).
4. `catalog pull <x>` prints an error but exits 0 — check output text, not
   exit code, until DF-019 lands.
5. `refresh <name>` → "get api: not found". Right way: use the ID shown in
   `musterflow list` (DF-024).
6. `-o csv` / `-o parquet` → "unsupported output format". Right way: table,
   json, yaml only (DF-020).

## What round 2 proved works (don't "fix" these)

- The full happy path end-to-end: connect (URL or file) → generated CLI calls →
  start → MCP tools/call (object endpoints) → flows (create/run/payload/webhook)
  → export/import → restart → everything persists. All verified live in one run.
- Table/json/yaml output render correctly for scalars; error bodies render
  table-style with exit 1; 302 redirects are followed; 401/404 surface cleanly.
- `auth add/list/remove` with `--data-dir` is now fully isolated (DF-003 fixed).
- Fresh `go build` from HEAD succeeds in ~40s (engine replace still machine-local).

## Round 4 (2026-09-10): why the remaining gaps exist

**The MCP stale-disconnect (DF-029) is a registry/derivation asymmetry.**
The tool registry is a snapshot rebuilt only on the ADD path (tools/list
reflects a new connect immediately — that fix landed), but DELETE removes
the API from the DuckDB registry without invalidating the snapshot, so
tools/list serves zombie tools until the process restarts. The robust shape
is to derive the tool list from the live registry on every tools/list
request (the registry is tiny); caching only makes sense with explicit
invalidation on disconnect. Any agent fixing this: do NOT add a
"refresh registry" CLI verb — the fix belongs server-side on the delete path.

**The workflow engine's missing API calls (DF-030) are structural, not an
oversight in one function.** `internal/workflow.Run()` constructs
`dsl.NewInterpreter(nil)`; muster's `pkg/dsl` registers ONLY what you pass
in that map, so flows get exactly `trigger` (predeclared) and `print`
(thread hook). There is no HTTP client builtin anywhere in the pipeline.
"Chain API calls" therefore requires new surface: register builtins
(e.g. `http_get(url, opts)`, or api-name/operationId dispatchers backed by
the connection registry) before the program compiles — Starlark resolves
undefined globals at compile time, which is why the failure is
`undefined: http_get` at flow COMPILE, not runtime. This is also why flows
can't break existing behavior when the builtins land: nothing references
them today.

**The install wall (DF-031/032) is a supply-chain decision, not a doc bug.**
`resolve-engine.sh` searches `$MUSTER_ENGINE_DIR`, `../muster`, `./muster`
and — finding nothing — prints an error and returns 0, so every scripted
consumer (CI, docker, a user's install script) marches into `go build` and
dies on a missing go.sum entry for the engine. Two independent fixes:
(a) the script must `exit 1` with one actionable line; (b) the README must
stop sending fresh users to a private clone — options are publishing the
engine, vendoring it, or shipping release binaries. Verified on a fresh
las-bunker-03 agent: public musterflow clone works ( proves the repo is
self-contained), engine clone fails with `could not read Username`, and a
bare Debian agent also lacks Go entirely — README never states the Go
version requirement.

**Routing overrides data-dir (DF-033) by design, silently.** The CLI
detects a dashboard on the configured port and forwards registry ops to it
(DF-001's fix). The forwarded request carries no data-dir identity, so a
user-specified `--data-dir` is simply ignored — success messages come from
the server's registry. Correct UX: if the explicit flag differs from the
routed server's dir, refuse or warn; `--no-dashboard` remains the escape
hatch for guaranteed local semantics.

**Round-4 regression scorecard:** DF-015 (MCP dynamic add) FIXED,
DF-016 (tools/call arrays) FIXED, DF-017 (array query params) FIXED,
DF-020 (output formats) FIXED, DF-024 (name-or-id) FIXED. Still open:
DF-029 (3rd run), flag asymmetry (3rd run, DF-034), catalog 500/404
divergence (2nd run, DF-035). The pattern across rounds: the foreman fixes
exactly what is re-filed with live evidence, and persistent findings are
the ones prior rounds filed without a reproduction recipe. File findings
with exact commands.

## Round 6 (2026-10-01): the agent-facing surfaces, and how auth was proven broken

**Two P1s live in the same seam: the MCP tool registry and the auth store.**

**MCP collisions (DF-039) are the naming half of DF-029's staleness.** Tools
are registered under bare operationIds. Round 6 connected a second API
reusing `listWidgets`/`createWidget`/… and tools/list served both — then
`tools/call listWidgets` dispatched to the SECOND API's base URL
(last-connect-wins map insert), silently breaking the first API's tool. The
CLI tree namespaces by API name (`<api> <group> <op>`); the MCP surface
doesn't. Fix shape: derive tool names `<api-slug>_<operationId>` (or keep
short names only when globally unique) AND re-derive the registry on every
tools/list (which is also DF-029's fix — one change closes both). Until it
lands: never connect two APIs that share operationIds against one server.

**The auth split-brain (DF-040) is a store mismatch between musterflow and
the engine, and only a wire test sees it.** musterflow's `auth add` writes
`config.yaml` (`auth:` map, plaintext). The generated commands' `--auth`
flag resolves through the engine's `muster/pkg/auth.ResolveStored` → the
PLATFORM KEYCHAIN (service `muster-cli`), which only the engine's own CLI
populates. Result: credentials stored by musterflow are never attached to
any request — a header-echo server received zero auth headers with the
credential stored under the API name, the API id, with or without `--auth`.
 musterflow also has an auto-resolve path (`ExecuteOptions.AuthManager` +
`APIID` in internal/cli/execute.go) but nothing on the live command path
constructs it — dead code. The engine-side bridge WORKS: `muster auth add
--name X --type api-key --value Y` (engine binary) then `musterflow <api>
<op> --auth X` sends `X-API-Key: Y`. So the fix is alignment, not new
plumbing: point `--auth` resolution at `auth.Manager` first (keychain
fallback), or make `auth add` write the keychain. Investigation traps worth
remembering: (1) `auth get`'s "masked-looking" output was the TERMINAL's
output redaction, not the file — byte-check (`od -c`, length) before
believing a mask; (2) exit codes are green on every auth path while no
credential moves — auth must be tested at the wire.

**Storage hygiene (DF-041):** `config.SaveWithDataDir` writes 0644
(internal/config/config.go:108) — a plaintext credential file that is
group/world readable. One-line fix (0600), do it with DF-040.

**`--no-dashboard` (DF-042) is not an escape hatch while the server runs:**
the DuckDB lock error surfaces raw ("Conflicting lock is held … registry
not loaded"). Detect the dashboard's lock and print one actionable line.

**What round 6 re-verified FIXED at HEAD (do not re-file):** DF-022 (dead
`--namespace/--watch` flags gone from leaf help), DF-024 (refresh accepts
name AND id), DF-028 (flow create writes the file), DF-032 (resolver exits
1 honestly — verified both paths in a scratch clone), DF-015 again (dynamic
ADD 24→29 tools live), DF-020 again (all six formats; parquet columns now
typed — read them back with DuckDB, id→DOUBLE). Dynamic completion reflects
a newly-connected API instantly.

**Install leg was SKIPPED this round (DF-043):** the bunker fleet refuses
all ssh keys and the local bunker CLI has zero registered servers, so no
fresh-machine install could be attempted — the leg stays open, not passed.

**Perf (round 6, hyperfine, warm unless noted):** generated API call 19ms
local / 12ms dashboard-routed; connect 26ms cold-fresh-HOME (bootstrap incl.)
/~10ms warm; nothing slow enough to file. Cold BINARY build remains the only
wait a user feels (~40s warm-cache, ~4min cold module cache).

## Round 7 — 2026-11-16 (HEAD 80119d1): the failure paths are the product now

Round 7 walked the happy paths again (public quickstart, a local CRUD API, the
dashboard/MCP/flows surface, and a 42-API registry) and found the happy paths
solid: connect 93 ms, first call 53 ms, 42 APIs cost +4.6 ms of CLI startup.
Everything uncomfortable lives where the network misbehaves. Three mechanisms
explain all of it, and each is worth understanding before touching that code.

**1. The retry policy does not know what idempotent means.** `internal/`
sends every request through one retry wrapper (`attempt 1..3`) that treats
`5xx` as "try again", regardless of method. That is safe for `GET`/`PUT`/
`DELETE` and dangerous for `POST`: a server that applies the write and *then*
answers 500 (commit-then-crash, timeout-after-write, a proxy rewriting the
response) leaves the client with a failed-looking call and no way to know the
write landed. Measured with a backend that records the side effect before
returning 500: one `create-item` invocation → **3 POSTs, 3 rows created (ids
2,3,4), command exit 1**. The right way: retry only idempotent methods by
default, or attach `Idempotency-Key` when the spec/API supports it, and give
the user `--no-retry` / `--retries N`. Do not "fix" this by disabling retries
globally — the same wrapper is what makes transient 502s invisible today.

**2. A stalled peer costs three fixed 30 s timeouts, and the signals that
should stop it are swallowed.** Against a TCP listener that accepts and never
answers, `list-items --base-url <blackhole>` ran **90.4 s** (3 × ~30 s) before
printing `TIMEOUT: … request failed after 3 attempts`. `SIGINT` sent at 3 s
did **not** shorten it (still 90.36 s), and `timeout -k 5 25` needed the
SIGKILL at 30.01 s — so Ctrl-C during a stall does nothing the user can see.
There is no knob: `config set timeout 5` answers `unknown config key: timeout
(valid: port, default_format, auto_completion, data_dir)`. The right shape is
one total deadline for a command (not per attempt) plus an interruptible
request context so an interrupt cancels the in-flight attempt instead of being
ignored by the retry loop.

**3. A registry that cannot be opened is reported as an empty registry.** The
Dashboard owns the DuckDB file (single-writer lock). When the CLI cannot
resolve a dashboard — the dashboard runs on a non-default port and `port` in
config was not updated — it falls back to local mode, fails to open the store,
prints the raw `Conflicting lock is held in … (PID n)` warning, and then keeps
going as if the registry were empty: `list` says **"No APIs connected." and
exits 0**, and every generated per-API command collapses into
`unknown command "<api>"`. That is the worst possible pair: a script asking
"what is connected?" gets a confident wrong answer, and an operator sees a
command-not-found instead of "your dashboard holds the lock, set `port` or
pass `--dashboard-addr`". The right shape is fail-closed: if the store cannot
be opened and no dashboard answers, exit non-zero with the actionable line
(and mention `config set port <dashboard-port>`), never invent an empty
registry. Note the correct usage is documented but nowhere near the error:
`musterflow config set port 19876` immediately restored routing in this run.

**Auth, re-proven (DF-040):** `auth add <api-id> --type bearer --key …` exits
0, `auth get <api-id>` returns byte-identical key material (sha256 and length
compared — the CLI did store the real secret, the terminal merely redacts
secret-looking output), and still **no `Authorization` header reaches the
target**. `--auth <api-id>` then says "credential not found" and advises
`auth add --name … --value …`, which the binary rejects (`unknown flag:
--name`). So a CLI-created credential can neither be attached by its own id
nor by any name a shipped command can create — the resolver and the writer
are two different stores, and only the engine-side keychain is readable.

**Install leg:** still SKIPPED (DF-043) — `bunker list` answers "no active
server; run 'bunker connect' first" and the host holds no bunkerd credential,
so no ephemeral fresh-machine install could be attempted this round either.

**Perf:** nothing user-noticeable in the happy path (numbers in the round-7
report); the single user-visible wait in the whole run is the 90 s stall above.

**Standing harness trap, for the next fire:** a probe backend must be
`ThreadingHTTPServer`. A single-threaded one plus a client that keeps
connections alive wedges after the first call, and every subsequent "the
product hangs" observation is the harness talking. The same fire also learned
that secret-looking strings are masked on the way to the terminal: verify
stored credentials with sha256/byte-length comparisons, never by eyeballing
the printed value.

### Round 8 (2026-11-23, HEAD d49da36) — how the auth era behaves in real use

Round 8 was the first fire to drive the post-3cf99b4 product: bearer-token
dashboard auth, the MCP JSON-RPC surface, and the flow HTTP builtins
(`http_get`/`http_post`/`http_request`/`sleep`/`json`) live on a dashboard.

**The design that works, and is worth copying forward:** one variable —
`MUSTERFLOW_DASHBOARD_TOKEN` (or `dashboard_token`) — means the same thing to
the server and the CLI. The CLI presents that token on every request it makes
to the dashboard (list, generated API subcommands even route their HTTP calls
through it). When it holds the token, everything just works; when it doesn't,
the server refuses with 401 JSON on exactly the routes specs/dashboard.md says
must be protected, and `/metrics` + `/api/health` stay genuinely exempt. The
loopback-only no-token mode prints a warning banner instead of silently
listening.

**The gap that DF-052 files:** the CLI's token is a spec-only fact. A user who
follows integration-guide.md sets `dashboard_token`, starts the dashboard, and
then finds every CLI command answering `unknown command "<api>"` after a
401-shaped fallback warning — the CLI-side contract (specs/cli.md) never made
it into the docs a person actually reads.

**Auth-adjacent residue:** the UI catch-all is deliberately inside the exempt
set, which makes `GET /api/nope` answer 200 HTML token-free (the open
API-CATCHALL row); the flow-not-found error echoes the flow file path
(`data/flows/nope.star`) — relative with a relative data-dir, absolute with an
absolute one (still-failing FLOWERR-PATHLEAK AC).

**Engine notes worth keeping:** flow response dicts carry exactly
`status`/`body`/`headers`, non-2xx is data (branch, don't catch — Starlark here
has no try), transport failures raise with the builtin prefix, sleep caps at
30s, and undefined globals are compile errors with file:line. Every one of
those documented claims was reproduced verbatim this fire — the docs engine
(docs/workflows.md, +475 lines in this delta) is the most accurate round 8 has
measured.

**Install leg:** the round-7-era "no active server" bunker state ended; the
battery launched detached on bunker-mvp (agent c686f0f2) — verdict recorded at
tick close in the dogfood log, not here.

**Harness trap of this fire:** a probe handler crashed on ANY empty-query
request (bad dict() over a one-element split list). The crash presents to the
client as EOF on POST/GET, which the product's own retry logic counts 3 times
before surfacing — the same EOF shape the round-7 DF-044 retry finding reads
from. If a retry observation depends on which side died, capture the server's
side per request before filing; a fixed harness can flip the finding.

