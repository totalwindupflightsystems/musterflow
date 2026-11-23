MusterFlow dogfood round 8 — evidence index (2026-11-23, HEAD d49da36)

Scratch run root: /home/kara/.hermes/cache/scratch/mf-dog-20261123 (pruned by the
scratch timer; the raw captures below are the durable copy). Scratch HOME and
--data-dir are isolated from the real ~/.musterflow. Probe backend: raw/probe_api.py
(OpenAPI CRUD "probe-pets" on 127.0.0.1:20081, ThreadingHTTPServer). Dashboard:
127.0.0.1:20981 with bearer token (dogfood-round8-secret). The token value is a
scratch-only throwaway, not a credential.

raw/ files
- auth_nohdr.json, badtoken.json — /api/flows without token / wrong token → 401 {"error":"unauthorized"} (bearer contract 3cf99b4 holds at HEAD)
- auth_withhdr.json — same route with token → 200 JSON
- html.json — GET / tokened → 200 dashboard HTML shell
- c1.json, c2.json — GET /api/nope untokened (401? NO: 200 HTML shell) and tokened (200 HTML) → API-CATCHALL row still live; c1 IS the dashboard shell, evidence that the auth-exempt UI catch-all serves unknown /api/* paths to unauthenticated callers as well
- hook_no.json — POST /hooks/nope tokened → 404 body 'read flow nope: open data/flows/nope.star: ...' → FLOWERR-PATHLEAK AC (no filesystem path) still unmet (relative-path echo reproduced in this fire)
- hook_run.json — POST /hooks/r8automation → 200 result string (webhook invocation path works)
- apirun.json — POST /api/flows/r8automation/run → 200 result string (dashboard invocation path works)
- h2.json — webhook run AFTER dashboard restart → 200 (persistence arm)
- fl.json — GET /api/flows tokened → five flows with sources and webhook flags
- ep_health — GET /api/health → {"connected_apis":1,"status":"ok"} (auth-exempt per spec)
- ep_metrics_ok.json, ep_metrics_untok.json — GET /metrics tokened and UNTOKENED both 200 Prometheus text (auth-exempt per specs/dashboard.md route table; not a finding)
- ep_mcpinfo.json — GET /api/mcp/info → api_count 1, tool_count 6 with generated examples
- mcpi.json — MCP initialize → 200 JSON-RPC, serverInfo musterflow-mcp 0.1.0
- mcpcall.json — MCP tools/call probe-pets__find-pets → 200 with real pet rows
- mcpu.json — MCP tools/call without token → 401 JSON (MCP is bearer-protected)
- spec.json — probe backend OpenAPI spec served at /openapi.json

Numbers (from /usr/bin/time, all rc captured):
build cold (module cache warm) 4.13s rc=0 | connect local spec COLD fresh data dir 0.22s rc=0 |
connect remote petstore v2 0.57s rc=0 | list warm 0.06s | CLI find-pets cold process 0.08s rc=0 |
flow run cold 1.03s rc=0 (contains sleep(1) → engine ~30ms) | flow run warm 1.03s
Nothing user-noticeably slow; no PERF row filed this fire.

CLI-vs-dashboard auth finding (DF-052) was not a raw file capture — the two
transcript strings ('Warning: could not fetch APIs from dashboard (HTTP 401);
falling back to local registry' / 'Error: unknown command "probe-pets"' and the
success run with MUSTERFLOW_DASHBOARD_TOKEN exported) are quoted verbatim in the
row reasoning and in docs/dogfood/2026-11-23-integration.md.

Run-record disclosure: the probe backend had TWO harness-side bugs fixed in flight
before any product arm ran (query-dict parse crashed empty-query GET, then POST,
both surfacing as EOF after the product's retry logic — retry counted 3 attempts
as designed). All product numbers above come from arms run against the FIXED
backend; the crash arms themselves validated product-side retry (3 attempts, then
a clean error) against a dying peer.
