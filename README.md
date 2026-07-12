# MusterFlow

**Turn any OpenAPI spec into a CLI, an MCP tool, and a workflow engine.**

[![Go Version](https://img.shields.io/badge/Go-1.26.1-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue)](./LICENSE)
[![Build](https://img.shields.io/badge/build-passing-brightgreen)](https://github.com/totalwindupflightsystems/musterflow)

Connect an OpenAPI spec → get an instant CLI with subcommands for every endpoint, an HTTP MCP server for AI agents, and a Starlark workflow engine for automation.

```
$ musterflow connect https://petstore3.swagger.io/api/v3/openapi.json
Connected: Swagger Petstore (19 endpoints)

$ musterflow swagger-petstore-openapi-3-0 listPets --limit 5
┌────┬───────────────┬────────┐
│ ID │ NAME          │ STATUS │
├────┼───────────────┼────────┤
│ 1  │ Bella         │ sold   │
│ 2  │ Max           │ avail  │
│ 3  │ Luna          │ pending│
│ 4  │ Charlie       │ avail  │
│ 5  │ Lucy          │ sold   │
└────┴───────────────┴────────┘

$ musterflow catalog search github
┌──────────────────┬─────────────────────────────────┬──────────┬───────┐
│ NAME             │ DESCRIPTION                     │ TYPE     │ SCORE │
├──────────────────┼─────────────────────────────────┼──────────┼───────┤
│ GitHub           │ GitHub REST API v3              │ official │ 10/10 │
│ GitHub Enterprise│ GitHub Enterprise Server API    │ official │ 9/10  │
└──────────────────┴─────────────────────────────────┴──────────┴───────┘
```

## Installation

### Go Install

```bash
go install github.com/totalwindupflightsystems/musterflow/cmd/musterflow@latest
```

### Homebrew

```bash
brew install musterflow
```

### From Source

```bash
git clone https://github.com/totalwindupflightsystems/musterflow.git
cd musterflow
go build -o musterflow ./cmd/musterflow/
```

## Quick Start

```bash
# 1. Connect an API by its OpenAPI spec URL
musterflow connect https://petstore3.swagger.io/api/v3/openapi.json

# 2. Call it from the CLI — subcommands generated automatically
musterflow swagger-petstore-openapi-3-0 listPets --status available

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
$ musterflow <api-name> <operation> --flag1 value --flag2 value
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

### 📦 Community Catalog — Share & Discover APIs

```bash
musterflow catalog search github     # Search for APIs
musterflow catalog push petstore     # Share your API connection
musterflow catalog pull github       # Install a community API
```

Pre-seeded with 10 APIs: GitHub, Stripe, Slack, Discord, OpenAI, Notion, Linear, Jira, Twilio, Cloudflare — all with verified OpenAPI spec URLs.

### 🔐 Auth — Per-API Credential Management

```bash
musterflow auth add gh --type apikey --key ghp_xxx
musterflow auth add db --type bearer --token eyJ...
musterflow auth login slack          # OAuth2 browser flow
musterflow auth add api --type mtls --cert client.pem --key client-key.pem
musterflow auth list                 # Keys are masked
```

Supports: API key, Bearer token, OAuth2 (authorization code with PKCE), and mTLS.

### ⚡ Workflows — Starlark Automation

Chain API calls together with Starlark scripts:

```bash
musterflow flow create new-issue-notify
# Edit ~/.musterflow/flows/new-issue-notify.star
musterflow flow run new-issue-notify --payload '{"repo":"my-repo"}'
```

Webhook triggers via `/hooks/` endpoint for event-driven automation.

### 🧠 DuckDB — Persistent, Concurrent-Ready Storage

API connections stored in DuckDB at `~/.musterflow/musterflow.db`. JSONL export/import for portability. Automatic migration from legacy JSON registry. Read-only CLI mode when the dashboard is running — no lock conflicts.

### 🎨 Quality Scoring — Know What You're Connecting

Automatic tier assignment based on: official domain (+5), endpoint count (+3), validated OpenAPI (+2), description presence (+1), examples (+1).

## Architecture

```
cmd/musterflow/main.go        → Entrypoint, config loading, server orchestration
internal/app/                 → API connection registry (DuckDB-backed)
internal/auth/                → Credential management (apikey, bearer, oauth2, mtls)
internal/catalog/             → Community catalog (search, push, pull, scoring)
internal/cli/                 → CLI command tree (cobra)
internal/config/              → YAML config (~/.musterflow/config.yaml)
internal/dashboard/           → Web dashboard + REST API
internal/mcp/                 → MCP HTTP server (JSON-RPC over SSE)
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
| `musterflow refresh <id>` | Re-fetch and update an API's spec |
| `musterflow start` | Start the dashboard and MCP server |
| `musterflow catalog search <q>` | Search the community API catalog |
| `musterflow catalog push <id>` | Push an API connection to the catalog |
| `musterflow catalog pull <id>` | Install an API from the catalog |
| `musterflow flow create <name>` | Create a Starlark workflow |
| `musterflow flow list` | List all workflows |
| `musterflow flow run <name>` | Execute a workflow |
| `musterflow auth add <id>` | Add credentials for an API |
| `musterflow auth list` | List configured auth (keys masked) |
| `musterflow auth remove <id>` | Remove credentials |
| `musterflow auth get <id>` | Retrieve a credential |
| `musterflow auth login <id>` | Start OAuth2 browser flow |
| `musterflow config show` | Print active configuration |
| `musterflow config set <k> <v>` | Update a config value |
| `musterflow export` | Export API registry as JSONL |
| `musterflow import` | Import API registry from JSONL |
| `musterflow completion bash\|zsh\|fish` | Install shell completions |

## Configuration

`~/.musterflow/config.yaml` on first run with defaults:

```yaml
port: 9876
data_dir: ~/.musterflow/
default_format: table
auto_completion: true
```

Port auto-discovery: if 9876 is occupied, tries 9877–9886. Override with `--dashboard-addr :9999` or by setting `port` in config.

## Shell Completion

Bash, Zsh, and Fish completion auto-installs on first run in interactive mode. Disable with `auto_completion: false` in config. Manual install:

```bash
musterflow completion bash | sudo tee /etc/bash_completion.d/musterflow
```

Dynamic completion: `musterflow gh <TAB>` shows GitHub API operations. Updates automatically when new APIs are connected.

## Output Formats

| Format | Flag | Example |
|--------|------|---------|
| Table (default) | `--format table` | Human-readable aligned columns |
| JSON | `--format json` | `[{"id": 1, "name": "Bella"}]` |
| YAML | `--format yaml` | Multi-document YAML |
| CSV | `--format csv` | Header row + comma-separated values |
| JSONL | `--format jsonl` | One JSON object per line |
| Parquet | `--format parquet` | Columnar, compressed |

Auto-detection from file extension: `--output data.csv` → CSV, `--output data.jsonl` → JSONL.

## License

MIT — see [LICENSE](./LICENSE).

## Links

- [Landing page](https://totalwindupflightsystems.github.io/musterflow-landing/)
- [Muster engine](https://github.com/wojons/muster) — core OpenAPI-to-CLI pipeline
- [GitHub repository](https://github.com/totalwindupflightsystems/musterflow)
