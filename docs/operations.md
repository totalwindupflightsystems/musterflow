# MusterFlow operations

The operational surface of the MusterFlow dashboard daemon: how it is built, how it is
supervised, what it exposes for monitoring, and how to tell a healthy process from one that
is accepting requests and failing them.

## What runs

| unit | entrypoint | default bind | data |
|---|---|---|---|
| `musterflow start` daemon | `cmd/musterflow` (`dashboard.NewServer`) | `127.0.0.1` / `:9876` (config `port`, `--dashboard-addr`) | `~/.musterflow` (`config.DataDir`) |

The daemon serves the dashboard SPA (`/`), the JSON API (`/api/...`), the MCP JSON-RPC
endpoint (`/mcp`), inbound webhooks (`/hooks/...`), a health route (`/api/health`) and the
metrics endpoint (`/metrics`).

## Build and toolchain

The workspace (`go.work`) replaces `github.com/wojons/muster` with a local checkout. That
module carries a newer `go` directive than the fleet host's default toolchain, so builds on
those hosts need an explicit toolchain pin:

```bash
GOTOOLCHAIN=go1.26.10 go build ./...
GOTOOLCHAIN=go1.26.10 go test ./...
```

Without the pin the build stops at `module /path/to/muster requires go >= 1.26.10
(running go 1.26.6)` — that is a toolchain message, not a code failure.

A worktree created with `git worktree add` does **not** inherit `go.work` / `go.work.sum`
(both are gitignored), so copy them into the worktree before building there, or the build
fails with `missing go.sum entry ... github.com/wojons/muster/pkg/...`.

## Run

```bash
musterflow start                       # foreground, Ctrl+C to stop
musterflow start --dashboard-addr :9876 --data-dir /var/lib/musterflow
```

`SIGINT`/`SIGTERM` trigger a graceful shutdown of the HTTP server.

## Deploy (container)

`docker-compose.yml` builds the image, publishes `9876:9876`, mounts the
`musterflow-data` volume at `/root/.musterflow` and sets `restart: unless-stopped` — Docker
is the supervisor and the restart policy, so no separate systemd unit is required:

```bash
docker compose up -d --build
docker compose logs -f musterflow
```

## Health

`GET /api/health` returns `{"status":"ok","connected_apis":N}`. It answers 200 whenever the
HTTP listener is alive; the connected-API count is the useful field (0 means a running
process with no usable catalog).

## Metrics

`GET /metrics` serves the Prometheus text exposition format (`version=0.0.4`) on the same
listener. It is unauthenticated like the rest of the local dashboard surface, so it is bound
to the same address — keep the listener on loopback or behind the reverse proxy, never
expose `:9876` directly to the internet.

Families exported (stable prefix `musterflow_`):

| metric | type | labels | meaning |
|---|---|---|---|
| `musterflow_http_requests_total` | counter | `method`, `path`, `code` | requests served, after route folding |
| `musterflow_jobs_total` | counter | `method`, `path`, `code` | non-GET calls on job routes: flow runs (`/api/flows/`), webhook triggers (`/hooks/`), MCP tool calls (`/mcp`) |
| `musterflow_errors_total` | counter | `path`, `reason` | failures by cause: `client_error` (4xx), `server_error` (5xx), `auth_denied` (401/403), `rate_limited` (429) |
| `musterflow_http_request_duration_seconds` | histogram | `path`, `le` | request latency, buckets 5 ms … 10 s |
| `musterflow_build_info` | gauge | `go_version` | always 1; identifies the running build |
| `musterflow_process_start_time_seconds` | gauge | — | process start, for restart detection |

Two properties make the series safe to store:

- **Path labels are route templates, not raw paths.** `/api/apis/abc123` reports as
  `/api/apis/`; any path served by the SPA catch-all reports as `/`. A caller looping over
  ids cannot mint unbounded series.
- **`/metrics` is never counted.** A scrape loop does not inflate the numbers it reports.

### Scrape configuration

```yaml
scrape_configs:
  - job_name: musterflow
    scrape_interval: 30s
    static_configs:
      - targets: ["127.0.0.1:9876"]
    metrics_path: /metrics
```

Alerts worth wiring: `rate(musterflow_errors_total{reason="server_error"}[5m]) > 0`,
`increase(musterflow_http_requests_total[10m]) == 0` while the process is up (listener alive,
traffic dead), and `changes(musterflow_process_start_time_seconds[1h]) > 0` (restart loop).

## Backup

The daemon owns one datastore: `~/.musterflow` (`config.yaml`, connected-API records, saved
flows, catalog cache). It is small and rebuildable for API metadata but **not** for saved
flows, so back up the directory:

```bash
tar -czf "musterflow-data-$(date +%F).tar.gz" -C "$HOME" .musterflow
```

Restore is the reverse: stop the daemon, unpack over `~/.musterflow`, start it, then check
`GET /api/health` reports the expected non-zero `connected_apis`. In the container deploy
the same directory lives on the `musterflow-data` volume
(`docker run --rm -v musterflow-data:/d -v "$PWD":/b alpine tar czf /b/musterflow-data.tar.gz -C /d .`).

## Troubleshooting

| symptom | first probe | usual cause |
|---|---|---|
| dashboard loads, all flows fail | `GET /api/health` connected count, `musterflow_errors_total{reason="server_error"}` | upstream API key or base URL changed |
| request spikes with no visible change | `musterflow_http_requests_total` by `path`, `musterflow_errors_total` by `reason` | a webhook or MCP client retry loop |
| process restarts repeatedly | `changes(musterflow_process_start_time_seconds[1h])`, `docker compose logs` | OOM or bad `config.yaml` |
| port conflict at start | `ss -ltnp \| grep 9876` | `FindPort` shifts the daemon up to 10 ports; the log line reports the real one |
