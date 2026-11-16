# MusterFlow — Round-7 real-use report (2026-11-16)

Run id: `musterflow-dogfood-2026-11-16-18-27-37`
Measured tree: `/home/kara/musterflow` @ `80119d1` (clean except untracked
scratch artifacts), binary built in-session with
`go build -o <scratch>/bin/musterflow ./cmd/musterflow/` (4.83 s warm cache,
Go 1.26.11, engine via the existing `go.work` replace to `/home/kara/wojons/muster`).
Host: 16 cores, 1-min loadavg 22–32 during the run.

**Verdict: 🟡 PROMISING-BUT-ROUGH** — the headline promise holds and is fast;
the friction is now entirely in the *failure* paths: retries that duplicate
writes, a stalled upstream that freezes the CLI for 90 s uninterruptibly, and
a registry fallback that reports an empty registry as if it were a true answer.

## Promise (Step 1)

"Connect an OpenAPI spec → an instant CLI with a subcommand per endpoint, an
HTTP MCP endpoint for AI agents, and a Starlark workflow engine — all from one
command" (README hero + `docs/integration-guide.md`).

## What was actually done (Step 2, real use, not the test suite)

Built the binary, then drove three real surfaces with an isolated
`--data-dir` and an isolated `HOME`:

1. **Public quickstart** — `connect https://petstore3.swagger.io/api/v3/openapi.json`
   → 19 endpoints, then `pet find-pets-by-status --status available` and
   `pet get-pet-by-id 1` in table/json/yaml. Reads worked; the upstream demo
   API answers **HTTP 500 on `POST /pet`** (upstream, not musterflow).
2. **Local deterministic CRUD API** (own OpenAPI 3.0 spec, 5 operations,
   request-logging backend) connected as `dogfood-items-api` and driven
   end-to-end: create → list → filter by `status` → get by id → update →
   delete → get-after-delete (404, rc=1). Every documented shape worked,
   including `--output json|yaml`, `--output-file`, `-b/--body`, and the
   `--tags 'a,b'` body-array form.
3. **Dashboard, MCP and flows** on `:19876`: `musterflow start`, `/api/health`,
   `/metrics`, full MCP `initialize` → `tools/list` (24 tools on 2 APIs,
   147 on 43 APIs) → `tools/call` (object **and** array responses), a
   `flow create`/`flow run` (`print(6*7)` → `42`), and a webhook flow
   (`POST /hooks/webhookflow` → `{"result":"42\ntrigger=set"}`), exactly as
   the integration guide documents.
4. **Scale and migration**: 40 extra generated specs connected (0 failures),
   42-API registry `export` → fresh `import` → 42 APIs, second import
   idempotent (still 42), restored registry executes API calls from a
   dashboard-less data dir.

**Time to first success** (pristine data dir, local spec): **146 ms median**
(connect 93 ms + first call 53 ms; 5/5 runs rc=0 with non-empty stdout).
Against the remote petstore URL the same connect took **0.56 s** including the
spec download.

## Findings filed

| id | sev | what |
|----|-----|------|
| PERF-001 | P1 | A stalled upstream freezes a single CLI call for **90.4 s** (3 attempts × 30 s) and **Ctrl-C / SIGINT is ignored**; no timeout knob exists (`config set timeout` → "unknown config key ... valid: port, default_format, auto_completion, data_dir") |
| DF-044 | P1 | HTTP **retry replays non-idempotent POST on 5xx**: one `create-item` produced **3 duplicate records** while the command still exited 1 |
| DF-045 | P1 | When a dashboard holds the DuckDB lock on a non-default port, the CLI falls back to a local registry, prints the raw lock error, then reports **"No APIs connected." with rc=0** and turns every generated command into `unknown command "<api>"` |

Notes attached to existing rows (no duplicate ids minted): **DF-040**
(wire-proof re-verified: `auth add` succeeds, `auth get` returns the exact
stored bytes — sha256 match — yet **no `Authorization` header ever reaches the
API**; `--auth <api-id>` answers "credential not found" and advises
`auth add --name … --value …`, flags the binary rejects with
`unknown flag: --name`) and **DF-043** (install-bunker leg still unrunnable:
`bunker list` → "no active server; run 'bunker connect' first", no credential
on the host).

## Measurements (Step 2b — perf)

Hyperfine, this host, 16 cores, loadavg 27–32 during the CLI arms:

| operation | runs | mean ± σ |
|---|---|---|
| `--help` (2 APIs connected) | 20 | 15.2 ms ± 2.5 ms |
| generated call `get-item`, dashboard-routed | 20 | 16.8 ms ± 2.5 ms |
| `connect <local spec>` (warm) | 10 | 127 ms ± 35.5 ms |
| `--help` (42 APIs connected) | 20 | 19.8 ms ± 3.3 ms |
| generated call (42 APIs connected) | 20 | 21.4 ms ± 3.0 ms |
| `flow run` | 1 | 20 ms |
| MCP `tools/list` (24 → 147 tools) | 1 each | ≤ 1 ms / 10 ms |

**Fleet-wide CLI cost of 40 extra connected APIs: +4.6 ms on `--help`, +4.7 ms
on a routed call** (~0.12 ms per API). Nothing in the happy path is slow
enough for a user to notice, so no PERF row was minted for it — the only
user-noticeable wait in the whole run is the 90 s stall path in PERF-001.
No profiler was justified (the 90 s is three fixed 30 s timeouts, not CPU).

## Positives re-verified at 80119d1 (do not re-file)

- MCP `tools/call` returns **array** responses correctly (DF-016 fixed): both
  `content[0].text` and `structuredContent.result` carry the full array.
- `/metrics` exists and is honest: bounded labels (`path="/api/apis/"`, route
  templates), request counters and duration histograms (MUSTERFLOW-OPS-001).
- stdout stays machine-clean: the `INF HTTP response received` line goes to
  **stderr**, so `... --output json | jq` parses.
- `export`/`import` round-trip preserves `id/spec_url/base_url/auth_type/
  endpoint_count`, and re-import is idempotent.
- `config show` masks stored keys (`Zz9-****-abc`), and `config set port N`
  makes the CLI route through a non-default dashboard port (the documented
  fix for the DF-045 scenario — the defect is that the CLI does not say so).
- 404 from the target API surfaces as a table row + `Error: HTTP error: 404`
  with **rc=1**.

## Boundaries (what this run did NOT verify)

- **Fresh-machine install leg: SKIPPED, not passed.** `bunker list` reports no
  active server and no credential is obtainable on this host, so
  `bunker-qa.sh` could not launch an ephemeral agent. Recorded on DF-043.
- **MCP timeout behaviour under a stalled upstream: UNVERIFIED.** The one
  observation that looked like a hang was caused by my own single-threaded
  probe backend and is excluded (see the diagnostics note); the CLI-side stall
  in PERF-001 is the proven arm.
- **OAuth2 / mTLS auth flows** were not exercised (no provider available);
  only bearer storage/resolution was probed.
- `--auth` never reaching the wire was re-proven, but the keychain-side
  (`muster auth add`) workaround written up in round 6 was **not** re-run.

## Harness lessons (my own bugs, not product bugs)

1. A single-threaded `HTTPServer` probe backend + a client that keeps
   connections alive = the backend wedges after the first call and every later
   "hang" is the harness. Switched to `ThreadingHTTPServer`; the MCP write
   then answered in 1.5 ms.
2. Credential values echoed through the terminal are redacted by the harness —
   a "the stored secret is masked!" hypothesis was refuted by comparing
   sha256 + byte length of the file value against the sha256 of the candidate
   secret (both 22 bytes, equal digests → the secret is intact).
