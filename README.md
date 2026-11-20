# MusterFlow

**Turn any OpenAPI spec into a CLI, an MCP tool, and a workflow engine.**

[![Go Version](https://img.shields.io/badge/Go-1.26.11-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue)](./LICENSE)
[![CI](https://github.com/totalwindupflightsystems/musterflow/actions/workflows/ci.yml/badge.svg)](https://github.com/totalwindupflightsystems/musterflow/actions/workflows/ci.yml)

Connect an OpenAPI spec → get an instant CLI with subcommands for every endpoint, an HTTP MCP server for AI agents, and a Starlark workflow engine for automation.

```
$ musterflow connect https://petstore3.swagger.io/api/v3/openapi.json
✓ Connected: swagger-petstore-openapi-3-0
  ID: 715e581e0d464caa
  Version: 1.0.27
  Endpoints: 19

$ musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available
id      name        status
87205   pet-87205   available
82037   pet-82037   available
66423   pet-66423   available

$ musterflow catalog search github
Error: catalog backend not available (HTTP 404): repo https://raw.githubusercontent.com/totalwindupflightsystems/musterflow-catalog/main absent or private
```

## Installation

### From Source

The `github.com/wojons/muster` engine is a published Go module, so the build pulls it from the module proxy — no engine checkout is needed:

```bash
git clone https://github.com/totalwindupflightsystems/musterflow.git
cd musterflow
go build -o musterflow ./cmd/musterflow/
```

> **Engine development:** to build against a local engine checkout instead of the published module, clone `wojons/muster` as a sibling (`../muster`, or point `MUSTER_ENGINE_DIR` at it) and run `bash scripts/resolve-engine.sh`. That generates a `go.work` with a workspace-level replace; `go.work`/`go.work.sum` are local, gitignored artifacts and `go.mod` is never edited. With no checkout present the script is informational only (exit 0) and the published module is used.

## Integration Guide

See [docs/integration-guide.md](docs/integration-guide.md) for the full integration guide (CLI, dashboard, MCP, workflow engine).

## Quick Start

```bash
# 1. Connect an API by its OpenAPI spec URL
musterflow connect https://petstore3.swagger.io/api/v3/openapi.json

#    petstore3 upstream is a third-party host that has had repeated 500
#    outages. If connect fails on it, any other OpenAPI spec works the same
#    way — e.g. the petstore v2 spec:
musterflow connect https://petstore.swagger.io/v2/swagger.json

# 2. Call it from the CLI — subcommands generated automatically
musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available

# 3. Start the dashboard and MCP server
musterflow start
# Dashboard:   http://localhost:9876
# MCP:         http://localhost:9876/mcp
# API:         http://localhost:9876/api/
```

## Features

### 🖥️ CLI — Instant Commands from API Specs

Every OpenAPI operation becomes a CLI subcommand. Path parameters, query flags, and request bodies are all handled:

```bash
$ musterflow <api-name> <resource> <operation> --flag1 value --flag2 value
```

Output formats: **table** (default), **JSON**, **YAML**, **CSV**, **JSONL**, and **Parquet**.

### 🤖 MCP Server — AI Agent Integration

Every connected API is registered as an MCP tool served over HTTP. AI agents discover and call them via JSON-RPC:

```bash
curl -X POST http://localhost:9876/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

Dynamic — connect a new API while the server is running and tools update without restart.

### 📊 Dashboard — Browse & Manage APIs

A dark-themed web dashboard at `http://localhost:9876`:
- View connected APIs with endpoint counts, auth type, and spec info
- Browse the community catalog with search and quality scoring
- MCP connection details with per-tool JSON-RPC examples

### 📦 Community Catalog — Share & Discover APIs (Experimental)

The community catalog backend (`totalwindupflightsystems/musterflow-catalog` GitHub repo) is **not yet available**. Catalog commands (`search`, `push`, `pull`) are implemented but the backend repo has not been created yet, so `catalog search` will report that the backend is unavailable.

```bash
# Once the catalog backend is available:
musterflow catalog search github       # Search for APIs
musterflow catalog push <api-id|name>  # Share a connected API (id or name from `musterflow list`)
musterflow catalog pull <api-id>       # Install a community API
```

> **Note:** `catalog push` accepts either the **api-id** or the **name** of a connected API (both shown by `musterflow list`).

### 🔐 Auth — Per-API Credential Management

```bash
musterflow auth add petstore --type apikey --key <token>
musterflow auth add db --type bearer --key eyJ...
musterflow auth login slack --client-id <id> --client-secret <secret> --auth-url <url> --token-url <url>   # OAuth2 browser flow (all four flags required; optional --scopes, --redirect-port)
musterflow auth add api --type mtls --cert client.pem --key-path client-key.pem
musterflow auth list                 # Keys are masked
```

Generated calls attach a stored credential with `--auth <api-id>`:

```bash
musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available --auth petstore
```

Supports: API key, Bearer token, OAuth2 (authorization code, no PKCE), and mTLS.
`--auth` attaches API key, Bearer, and OAuth2 secrets as headers; mTLS is
transport-level and is configured on the call that connects the API.

### ⚡ Workflows — Starlark Automation

Chain API calls together with Starlark scripts. Every flow is predeclared with four globals: `trigger` (the run payload, or `None`), `http_get`, `http_post` and `http_request`.

```bash
musterflow flow create new-issue-notify
# Edit ~/.musterflow/flows/new-issue-notify.star
```

```python
# ~/.musterflow/flows/new-issue-notify.star
# Starlark has no top-level if/for, so put logic in a function and call it.

def main():
    resp = http_get("https://api.github.com/repos/totalwindupflightsystems/musterflow",
                    headers={"Accept": "application/vnd.github+json"})
    print("status:", resp["status"])
    print("content-type:", resp["headers"]["Content-Type"])

    note = http_post("https://httpbin.org/post",
                     body="repo=" + trigger["repo"],
                     headers={"Content-Type": "text/plain"})
    print("posted:", note["status"])

main()
```

```bash
musterflow flow run new-issue-notify --payload '{"repo":"my-repo"}'
```

Each call returns a dict with exactly `status` (int), `body` (string, capped at 1 MiB) and `headers` (first value per header name). Non-2xx statuses are returned, not raised, so flows can branch on them; only connection/DNS/timeout failures raise. Requests time out after 10s.

Webhook triggers via `/hooks/` endpoint for event-driven automation.

### 🧠 DuckDB — Persistent, Concurrent-Ready Storage

API connections stored in DuckDB at `~/.musterflow/musterflow.db`. JSONL export/import for portability. Automatic migration from legacy JSON registry. When the dashboard is running, CLI commands route through its HTTP API automatically — no direct DuckDB access, no lock conflicts.

### 🎨 Quality Scoring — Know What You're Connecting

Automatic tier assignment. A catalog entry scores 0-10 from: official domain (+5), structured OpenAPI spec — `type: openapi` (+2), description longer than 20 characters (+1), non-empty version (+1), more than 100 downloads (+1). Tiers: score ≥ 7 → `official`, ≥ 3 → `community-inferred`, otherwise `untested`.

## Architecture

```
cmd/musterflow/main.go        → Entrypoint, config loading, server orchestration
internal/app/                 → API connection registry (DuckDB-backed)
internal/auth/                → Credential management (apikey, bearer, oauth2, mtls)
internal/catalog/             → Community catalog (search, push, pull, scoring)
internal/cli/                 → CLI command tree (cobra)
internal/config/              → YAML config (~/.musterflow/config.yaml)
internal/dashboard/           → Web dashboard + REST API
internal/mcp/                 → MCP HTTP server (HTTP POST JSON-RPC)
internal/wasm/                → WASM transform sandbox
internal/workflow/            → Starlark workflow engine
internal/completion/          → Shell completion (bash, zsh, fish)
```

## Commands

| Command | Description |
|---------|-------------|
| `musterflow connect <url>` | Connect an API from its OpenAPI spec URL |
| `musterflow list` | List all connected APIs |
| `musterflow disconnect <id>` | Remove a connected API |
| `musterflow refresh <id|name>` | Re-fetch and update an API's spec |
| `musterflow start` | Start the dashboard and MCP server |
| `musterflow catalog search <q>` | Search the community API catalog (experimental — backend not yet available) |
| `musterflow catalog push <id|name>` | Push an API connection to the catalog |
| `musterflow catalog pull <id>` | Install an API from the catalog |
| `musterflow flow create <name>` | Create a Starlark workflow |
| `musterflow flow list` | List all workflows |
| `musterflow flow run <name>` | Execute a workflow |
| `musterflow auth add <id>` | Add credentials for an API |
| `musterflow auth list` | List configured auth (keys masked) |
| `musterflow auth remove <id>` | Remove credentials |
| `musterflow auth get <id>` | Retrieve a credential |
| `musterflow auth login <id>` | Start OAuth2 browser flow (requires `--client-id`, `--client-secret`, `--auth-url`, `--token-url`) |
| `musterflow config show` | Print active configuration |
| `musterflow config set <k> <v>` | Update a config value |
| `musterflow export` | Export API registry as JSONL |
| `musterflow import <file>` | Import API registry from JSONL |
| `musterflow completion bash\|zsh\|fish` | Install shell completions |

## Configuration

`~/.musterflow/config.yaml` on first run with defaults:

```yaml
port: 9876
data_dir: /home/<user>/.musterflow
default_format: table
auto_completion: true
```

`data_dir` is stored as the **resolved absolute path** (no trailing slash): the first-run default is written that way. A `~` you set yourself is NOT expanded — `config set data_dir '~/.musterflow'` stores the literal `~`, and the app then creates a `~` directory relative to the current working directory. Always pass an absolute path. Scripts should read the resolved value.

The data directory holds everything MusterFlow writes:

```
~/.musterflow/
├── config.yaml            # User config — also holds per-API credentials (auth: map)
├── musterflow.db          # DuckDB registry (API connections)
├── flows/                 # Starlark flows: <name>.star + <name>.star.json metadata sidecar
├── transforms/            # WASM transforms, read by `transform list`
└── tokens/                # OAuth2 token cache from `auth login` (<client-id>.yaml, mode 0600)
```

`flows/<name>.star` holds only Starlark source; the `<name>.star.json` sidecar next to it holds the flow's `description` and `webhook` flag, which is what `flow list` and the webhook route read back. A flow whose sidecar is missing (created by an older MusterFlow) lists with an empty description and as non-webhook — that is a fallback, not an error. `transforms/` and `tokens/` always resolve under the default `~/.musterflow`, even when `--data-dir` points elsewhere.

**The dashboard is the writer.** `musterflow start` serves the registry and holds the single-writer DuckDB lock on `<data_dir>/musterflow.db`. When a dashboard answers on the configured port, CLI commands go over its HTTP API instead of opening the file, so they mix with a running server without a lock conflict; if it serves a different port, `--dashboard-addr 127.0.0.1:<port>` (or the `port` config key) restores routing. A separate `--data-dir` gives you its own `musterflow.db` and lock for offline work.

Port auto-discovery: if another program already holds the configured port, MusterFlow tries the nine ports above it (`port+1` … `port+9` — 9877–9885 for the default 9876) and serves on the first free one, printing which one it chose. A port held by another MusterFlow dashboard is never bypassed: `start` refuses and tells you to reuse it. Override with `--dashboard-addr :9999` or by setting `port` in config.

## Shell Completion

Bash, Zsh, and Fish completion auto-installs on first run in interactive mode. Disable with `auto_completion: false` in config. Manual install:

```bash
musterflow completion bash | sudo tee /etc/bash_completion.d/musterflow
```

Dynamic completion: `musterflow swagger-petstore-openapi-3-0 <TAB>` shows the API's resources and operations. Updates automatically when new APIs are connected.

## Output Formats

Generated API commands take `-o/--output <fmt>` to select the format and the persistent
`--output-file <path>` flag for the destination (stdout when omitted):

```bash
# Format only
musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available -o json

# Format to a file — the extension picks the format
musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available --output-file pets.csv
```

| Format | Flag | Output |
|--------|------|--------|
| Table (default) | `-o table` | Human-readable aligned columns |
| JSON | `-o json` | Pretty-printed JSON |
| YAML | `-o yaml` | YAML list/object |
| CSV | `-o csv` | Header row + comma-separated values |
| JSONL | `-o jsonl` | One JSON object per line |
| Parquet | `-o parquet` | Columnar, typed (INT64/DOUBLE/BOOLEAN/STRING) — binary, use `--output-file` |

Resolution order: an explicit `-o` sets the format, `--output-file` sets the destination,
then the format falls back to the file extension, then to `table`.

Extension auto-detection covers `.csv`, `.jsonl`, and `.parquet` — `--output-file data.csv`
writes CSV. For a file in another format, pass the format explicitly:
`-o json --output-file data.json`.

## Testing

```bash
# The suite CI runs
go test -short -count=1 ./...

# Full suite
go test -count=1 ./...
```

CI (`.github/workflows/ci.yml`) runs `go build ./...`, `go vet ./...` and the short suite
on every push and pull request to `master`.

## License

MIT — see [LICENSE](./LICENSE).

## Links

- [Landing page](https://totalwindupflightsystems.github.io/musterflow-landing/)
- [Muster engine](https://github.com/wojons/muster) — core OpenAPI-to-CLI pipeline
- [GitHub repository](https://github.com/totalwindupflightsystems/musterflow)
