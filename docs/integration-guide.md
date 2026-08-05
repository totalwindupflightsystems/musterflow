# MusterFlow Integration Guide

This guide shows how to integrate with MusterFlow across its four surfaces:

| Surface | Entry point | Port |
|---|---|---|
| CLI | `musterflow` binary (generated subcommands per connected API) | — |
| Dashboard + HTTP API | `musterflow start` | `9876` (default) |
| MCP server | `POST http://localhost:9876/mcp` (JSON-RPC 2.0) | `9876` |
| Workflow engine | `musterflow flow` commands (Starlark) | — |

All examples below were captured against a live MusterFlow instance (v1.0.27).

---

## 1. Quickstart

```bash
# 1. Connect an API from an OpenAPI spec
musterflow connect https://petstore3.swagger.io/api/v3/openapi.json
# → Version: 1.0.27
#   Endpoints: 19
#   Base URL: https://petstore3.swagger.io/api/v3
#   Try: musterflow swagger-petstore-openapi-3-0 --help

# 2. Call a generated endpoint subcommand
musterflow swagger-petstore-openapi-3-0 listPets --limit 5

# 3. Start the dashboard
musterflow start            # serves http://localhost:9876

# 4. Create and run a workflow
musterflow flow create hello --source "print(6 * 7)"
musterflow flow run hello   # → 42
```

> **Note:** the `replace github.com/wojons/muster => /home/kara/muster` directive in
> `go.mod` is a local-development pin. When building from source on another machine,
> see the note in `docs/` and the README "From Source" section.

---

## 2. CLI

### Global flags

Every command accepts:

| Flag | Default | Purpose |
|---|---|---|
| `--data-dir` | `~/.musterflow` | Data directory (API registry, flows) |
| `--dashboard-addr` | port from config | Dashboard HTTP address |
| `-o, --output` | — | Output file path (format from extension) |

### Top-level commands

| Command | Purpose |
|---|---|
| `musterflow connect <spec-url-or-file>` | Connect an API from an OpenAPI spec |
| `musterflow list` | List connected APIs |
| `musterflow disconnect <api>` | Disconnect an API |
| `musterflow auth` | Manage API credentials (OAuth2) |
| `musterflow config` | Manage MusterFlow configuration |
| `musterflow catalog` | Community catalog operations |
| `musterflow export` | Export the API registry to JSONL |
| `musterflow flow` | Workflow management (see below) |

### Generated subcommands

Each connected API becomes a command group named after the spec's title, with one
subcommand per endpoint. Example (Petstore, 19 endpoints):

```bash
musterflow swagger-petstore-openapi-3-0 --help
musterflow swagger-petstore-openapi-3-0 listPets --limit 5
musterflow swagger-petstore-openapi-3-0 getPetById --petId 1
```

### Routing behavior

When the dashboard is running on the configured port (default `9876`), CLI
`flow` commands route through the dashboard HTTP API automatically (port-liveness
detection). When no dashboard is running, they operate directly on the local data
directory.

---

## 3. Dashboard + HTTP API

Start the dashboard:

```bash
musterflow start --data-dir /tmp/my-muster-data
# → http://localhost:9876
```

The dashboard serves an HTML UI plus a JSON API. API responses wrap their data in
a named object (`{"apis": [...]}`, `{"flows": [...]}`).

| Endpoint | Method | Purpose |
|---|---|---|
| `/health` | GET | Health check; returns the dashboard HTML |
| `/api/apis` | GET | List connected APIs |
| `/api/apis` | POST | Connect an API (body: OpenAPI spec URL) |
| `/api/flows` | GET | List workflows |
| `/api/flows` | POST | Create a workflow |
| `/api/flows/<name>/run` | POST | Run a workflow |
| `/hooks/<name>` | POST | Trigger a webhook-enabled workflow (307 from `/hooks` → `/hooks/`) |
| `/mcp` | POST | MCP JSON-RPC endpoint (see §4) |

Example:

```bash
curl http://localhost:9876/health          # dashboard HTML
curl http://localhost:9876/api/flows       # {"flows":[]}
```

> `/hooks` redirects (307) to `/hooks/` and returns `400 {"error":"missing flow name"}`
> without a flow name — that is the designed contract. Unknown flow names return 404.

---

## 4. MCP server

MusterFlow exposes an MCP (Model Context Protocol) JSON-RPC 2.0 endpoint at
`/mcp` on the dashboard port. Point any MCP client at
`http://localhost:9876/mcp`.

```bash
curl -X POST http://localhost:9876/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

```json
{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}
```

---

## 5. Workflow engine (Starlark)

Workflows are Starlark scripts. Create, list, and run them with the CLI:

```bash
# Create (inline source)
musterflow flow create hello \
  --source "name = 'world'
print('hello, ' + name)
print(6 * 7)" \
  --description "greeting demo flow"

# → ✓ Created flow "hello"
#   Description: greeting demo flow
#   Edit: ~/.musterflow/flows/hello.star

musterflow flow list     # → Workflows: hello — greeting demo flow
musterflow flow run hello
# → hello, world
#   42
```

| Flag | Purpose |
|---|---|
| `--source` | Inline Starlark source for the flow (not a file path) |
| `--description` | Human-readable description, persisted in flow metadata |
| `--webhook` | Create a webhook trigger for the flow |

### Webhooks

Webhook-enabled flows receive a `trigger` payload global. Guard with
`if trigger != None` for flows that also run without a trigger:

```bash
musterflow flow create hook --webhook --source "if trigger != None:
    print('got ' + trigger.get('name', 'unknown'))"
curl -X POST http://localhost:9876/hooks/hook \
  -H 'Content-Type: application/json' \
  -d '{"name":"ada"}'
# → got ada
```

---

## 6. WASM transforms (optional)

MusterFlow can apply WASM transforms to API payloads. Transform modules follow
the WASI convention: read JSON from stdin, write the transformed JSON to stdout.
Configure a transform per connected API through `musterflow config` (see
`internal/wasm`).

---

## 7. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `flow create` writes `Edit: ~/.musterflow/flows/...` | CLI ran local-only; pass `--data-dir` to isolate test data |
| CLI flow commands get HTML/404 responses | Another process is squatting the configured dashboard port; free port `9876` or change the port in config |
| `parse flow <name>: ... want primary expression` | The flow body is not valid Starlark — check `--source` was passed as inline source, not a file path |
| Fresh clone fails to build | The `go.mod` local `replace` for the engine — see §1 note |
