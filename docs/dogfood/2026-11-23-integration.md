# Dogfood Round 8 — 2026-11-23 (HEAD d49da36)

**Promise under test:** "Connect an OpenAPI spec → get an instant CLI with
subcommands for every endpoint, an HTTP MCP server for AI agents, and a Starlark
workflow engine for automation."

**Environment:** isolated scratch HOME + `--data-dir` (real `~/.musterflow`
untouched), a fresh `go build` (72.7 MB binary, 4.13 s), and a local probe API
(`probe-pets`, OpenAPI CRUD on 127.0.0.1:20081). Dashboard started with a bearer
token on :20981. Evidence: `.coding-hermes/evidence/2026-11-23-dogfood-r8/`.

## What rounds 1–7 never drove, driven this fire

1. **Bearer-auth dashboard contract (landed 3cf99b4, after round 7).** No token
   → `401 {"error":"unauthorized"}` on `/api/flows` and `/mcp`; wrong token →
   401; valid token → 200. `GET /hooks/<name>` → 405, untokened GET → 401
   (POST-only contract holds). `/metrics` and `/api/health` are auth-exempt and
   that is what specs/dashboard.md says they must be. The new MCP surface works
   end to end: initialize → `musterflow-mcp` serverInfo → `tools/call
   probe-pets__find-pets` returned real rows. **Holds live.**
2. **The three documented flow-invocation paths in one session.** CLI
   `flow run`, `POST /api/flows/<name>/run`, and `POST /hooks/<name>` all
   executed the same builtins flow (`http_get` → `sleep` → `http_post` →
   `json.decode`) and returned the print-captured result string. Webhook runs
   still worked after a dashboard restart (registry, flows, webhook survived).
3. **New workflow builtins against their docs.** docs/workflows.md's claims
   were checked arm by arm and every one held: responses carry exactly
   `status`/`body`/`headers`; non-2xx is data (404 flows as a normal dict);
   transport failures raise; `sleep(31)` → verbatim
   `sleep: seconds must be at most 30s, got 31`; GET-with-body → verbatim
   `http_request: GET requests must not include a body`; undefined global →
   verbatim compile error with file:line. The engine's error messages are
   contract-grade.
4. **Catalog fix (d496d8d) in real use.** `catalog list` → 10 real entries;
   `catalog search github` filters correctly. Round 5's finding that the whole
   catalog family answered backend-404 is fixed at HEAD.
5. **CLI-against-dashboard with the lock hint.** Running CLI commands while the
   dashboard holds DuckDB now yields a genuinely actionable error (names the PID,
   the cause, and the exact `config set port` / `--dashboard-addr` remedy) —
   the DF-045 class is properly fixed. With `--dashboard-addr`, CLI commands
   route through the dashboard's HTTP API as integration-guide.md says.

## What broke / friction this fire

**DF-052 (filed, P2): the CLI cannot authenticate to its own tokened dashboard.**
`musterflow list` and every generated API subcommand, run while a tokened
dashboard owns the registry: `Warning: could not fetch APIs from dashboard
(HTTP 401); falling back to local registry` → `Error: dashboard returned HTTP
401` for `list`, but for a generated subcommand the run ends
`Error: unknown command "probe-pets"` — the whole command tree vanishes and the
user is sent hunting for a typo. The CLI-side fix exists and works: export
`MUSTERFLOW_DASHBOARD_TOKEN` (it is the same var) and identical commands succeed
through the dashboard. Nothing a user reads (integration-guide.md, operations.md)
says so — the token is documented server-side only; the CLI-side contract lives
only in specs/cli.md.

**Re-verified still live at HEAD (this fire's probes, noted on their rows):**
- REVIEW-MUSTERFLOW-20261121-API-CATCHALL: `GET /api/nope` → 200 text/html
  dashboard shell, tokened AND token-free — the JSON-404 contract for unknown
  `/api/*` paths is still open, and the auth-exempt UI catch-all means an
  unauthenticated caller gets the shell too.
- REVIEW-MUSTERFLOW-20261121-FLOWERR-PATHLEAK: `POST /hooks/nope` → 404 body
  `read flow nope: open data/flows/nope.star: ...` — no absolute host path
  reproduced with a relative data-dir, but a filesystem path still appears in
  the body; the row's "no filesystem path" AC is unmet either way.
- PERF-001: `config set timeout` still refuses with the same five-key
  vocabulary; no timeout knob exists.

**Verified fixed at HEAD (do not re-file): DF-045's masking (lock hint now
actionable), DF-051's output help (all six formats), the catalog backend, and
the empty-registry lie.**

## Verdict

🟡 **PROMISING-BUT-ROUGH**, closest to shippable of all eight rounds. The happy
path is genuinely fast (connect 0.22 s cold local / 0.57 s remote; CLI call
0.05–0.08 s; flow engine ~30 ms over sleep(1)) and docs+errors matched the
product on every arm this fire, which round 7 could not say. What keeps the label
is trust, not features: unknown `/api/*` paths still answer 200 HTML, the 90 s
uninterruptible stall still has no timeout knob, and the product's newest auth
surface degrades its own CLI into `unknown command` instead of naming the token.
