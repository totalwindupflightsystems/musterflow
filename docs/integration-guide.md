# MusterFlow Integration Guide

A comprehensive developer guide covering all four surfaces of MusterFlow: the **CLI**, the **dashboard HTTP API**, the **MCP server**, and the **Starlark workflow engine**. Every example is pre-verified against a running MusterFlow instance.

---

## Quickstart

Build from source and connect your first API:

```bash
git clone https://github.com/totalwindupflightsystems/musterflow.git
cd musterflow
go build -o musterflow ./cmd/musterflow/

# Connect an OpenAPI spec — subcommands are generated automatically
./musterflow connect https://petstore3.swagger.io/api/v3/openapi.json
```

Output:

```
Connected: Swagger Petstore (19 endpoints)
```

Now every operation in that spec is a CLI subcommand:

```bash
$ musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available
```

(Output is a formatted table—see the README for the full sample.)

---

## CLI Reference

The CLI binary (`musterflow`) is the primary interface. All commands respect two global flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--data-dir` | `~/.musterflow` | Directory for config, credentials, and DuckDB registry |
| `--dashboard-addr` | `:9876` | Address of a running dashboard; CLI routes through it when present |

### Connecting APIs

```bash
musterflow connect <openapi-spec-url>
```

Connects an API by its OpenAPI spec URL. MusterFlow fetches the spec, parses every endpoint, and generates CLI subcommands named after the spec title. The connection is persisted in DuckDB at `~/.musterflow/musterflow.db`.

### Listing Connected APIs

```bash
musterflow list
```

### Flow Commands

```bash
# Create a new Starlark workflow (--source takes inline Starlark source text, not a file path)
musterflow flow create --name myflow --source 'print(6 * 7)' --description "My first flow"

# List all workflows
musterflow flow list

# Run a workflow
musterflow flow run myflow
```

A flow whose `.star` source prints output returns it from `flow run`. Webhook-triggered flows are created with the `--webhook` flag; the webhook payload is available inside the Starlark script as the `trigger` global (a top-level `if trigger != None:` guard does not compile — `trigger` must be used inside a function body).

### Dashboard Control

```bash
musterflow start      # Start the dashboard/MCP server (default :9876)
```

When the dashboard is running on the configured port, CLI commands route through the dashboard's HTTP API automatically—no direct DuckDB access, no lock conflicts.

### Additional Commands

| Command | Description |
|---------|-------------|
| `musterflow disconnect <id>` | Remove a connected API |
| `musterflow refresh <id>` | Re-fetch and update an API's spec |
| `musterflow catalog search <q>` | Search the community catalog |
| `musterflow catalog push <id>` | Share an API connection |
| `musterflow catalog pull <id>` | Install a community API |
| `musterflow auth add <id>` | Add credentials (apikey, bearer, oauth2, mtls) |
| `musterflow auth list` | List configured credentials (keys masked) |
| `musterflow auth login <id>` | OAuth2 browser flow |
| `musterflow config show` | Print active configuration |
| `musterflow export` / `import` | JSONL export/import of the API registry |

---

## Dashboard HTTP API

Start the server with `musterflow start`. The dashboard, REST API, MCP endpoint, and webhooks all share port **9876** by default.

### Endpoints

```
GET  /api/health                   → 200  {"status":"ok","connected_apis":N}
GET  /                              → 200  (dashboard HTML)
GET  /api/apis                      → 200  {"apis":[...]}
POST /api/apis                      → 201  connect an OpenAPI spec
GET  /api/apis/<id>                 → 200  single API details
DELETE /api/apis/<id>               → 200  disconnect an API
POST /api/apis/<id>/refresh         → 200  refresh an API's spec (returns version/endpoint diff)
GET  /api/flows                     → 200  {"flows":[...]}
POST /api/flows                     → 201  create a flow
POST /api/flows/<name>/run          → 200  run a flow
POST /hooks/<name>                  → 200  trigger a webhook-enabled flow
GET  /hooks                         → 307  redirect to /hooks/ (flow name required)
GET  /api/catalog/search?q=<query>  → 200  catalog search
GET  /api/mcp/info                  → 200  MCP endpoint info and tool list
```

### JSON Response Shapes

**GET /api/apis** — list of connected APIs:

```json
{
  "apis": [
    {
      "id": "swagger-petstore-openapi-3-0",
      "name": "Swagger Petstore",
      "spec_url": "https://petstore3.swagger.io/api/v3/openapi.json",
      "base_url": "https://petstore3.swagger.io/api/v3",
      "auth_type": "",
      "endpoint_count": 19
    }
  ]
}
```

**POST /api/apis** — connect a new API (request body):

```json
{
  "spec_url": "https://petstore3.swagger.io/api/v3/openapi.json",
  "base_url": "",
  "name": "",
  "auth_type": ""
}
```

Response (201):

```json
{
  "id": "swagger-petstore-openapi-3-0",
  "name": "Swagger Petstore",
  "spec_title": "Swagger Petstore - OpenAPI 3.0",
  "spec_version": "3.0.2",
  "endpoint_count": 19,
  "base_url": "https://petstore3.swagger.io/api/v3"
}
```

**GET /api/flows** — list of workflows:

```json
{
  "flows": [
    {
      "name": "myflow",
      "description": "My first flow",
      "source": "print(6 * 7)",
      "webhook": false
    },
    {
      "name": "webhookflow",
      "source": "print(42)",
      "webhook": true,
      "webhook_url": "http://localhost:9876/hooks/webhookflow"
    }
  ]
}
```

**POST /api/flows/<name>/run** — run result:

```json
{
  "result": "42"
}
```

---

## MCP Server

The MCP server speaks **JSON-RPC 2.0 over HTTP POST** at `http://localhost:9876/mcp`. Every connected API is registered as a tool automatically. Connect a new API while the server is running and tools update without a restart.

### List Tools

**Request:**

```bash
curl -X POST http://localhost:9876/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

**Response (200):**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "tools": [
      {
        "name": "findPetsByStatus",
        "description": "Multiple status values can be provided with comma separated strings.",
        "inputSchema": {
          "type": "object",
          "properties": {
            "status": { "type": "string" }
          }
        }
      }
    ]
  }
}
```

> **Note:** MCP tool names are bare OpenAPI `operationId`s (e.g. `findPetsByStatus`, `loginUser`, `getUserByName`) with no API-name prefix. Because the MCP server registers every connected API's operations in a flat namespace, operationIds that collide across different APIs will overwrite each other — connect APIs with unique operationIds or be aware of the collision risk.

### Call a Tool

**Request:**

```bash
curl -X POST http://localhost:9876/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"findPetsByStatus","arguments":{"status":"available"}}}'
```

**Response (200):**

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "[{\"id\":1,\"name\":\"Bella\",\"status\":\"available\"},{\"id\":2,\"name\":\"Max\",\"status\":\"available\"},{\"id\":3,\"name\":\"Luna\",\"status\":\"available\"}]"
      }
    ]
  }
}
```

### MCP Info

```bash
curl http://localhost:9876/api/mcp/info
```

Returns the MCP endpoint URL, transport type, tool count, and per-tool name/description/example.

---

## Workflow Engine

MusterFlow embeds a Starlark workflow engine. Workflows are `.star` scripts stored in `~/.musterflow/flows/`.

### Creating and Running a Flow

```bash
$ musterflow flow create --name myflow --source 'print(6 * 7)' --description "My first flow"
$ musterflow flow run myflow
42
```

The CLI `flow run` prints the Starlark script's output to stdout. The dashboard API (`POST /api/flows/<name>/run`) returns the same output wrapped in a JSON object: `{"result": "42"}`.

### Webhook-Triggered Flows

A flow can receive an external webhook payload. Create it with the `--webhook` flag:

```bash
$ musterflow flow create --name webhookflow --source 'print(42); print("trigger=" + ("none" if trigger == None else "set"))' --webhook
```

The `trigger` global inside the Starlark script receives the parsed JSON payload. A top-level `if trigger != None:` guard does not compile in the Starlark engine ("if statement not within a function") — use `trigger` directly inside a function body or in a conditional expression as shown above.

When this flow is created, its `webhook_url` field is populated (e.g. `http://localhost:9876/hooks/webhookflow`). Non-webhook flows omit `webhook_url` entirely.

Trigger the flow via HTTP:

```bash
curl -X POST http://localhost:9876/hooks/webhookflow \
  -H "Content-Type: application/json" \
  -d '{"event":"push","repo":"my-repo"}'
```

The `trigger` global inside the Starlark script receives the parsed JSON payload.

---

## Example Walkthrough — End to End

### 1. Connect an API

```bash
$ musterflow connect https://petstore3.swagger.io/api/v3/openapi.json
Connected: Swagger Petstore (19 endpoints)
```

### 2. Call it from the CLI

```bash
$ musterflow swagger-petstore-openapi-3-0 pet find-pets-by-status --status available
```

(Formatted table output — see the README for the sample.)

### 3. Start the Dashboard

```bash
$ musterflow start
```

Dashboard is now serving at `http://localhost:9876`. In another terminal:

```bash
# Health check
$ curl http://localhost:9876/api/health
{"connected_apis":1,"status":"ok"}

# List connected APIs
$ curl http://localhost:9876/api/apis
{"apis":[...]}

# List MCP tools
$ curl -X POST http://localhost:9876/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

### 4. Create and Run a Workflow

```bash
$ musterflow flow create --name myflow --source 'print(6 * 7)' --description "My first flow"
$ musterflow flow run myflow
42
```

### 5. Browse the Dashboard

Open `http://localhost:9876` in a browser to see the dark-themed dashboard: connected APIs, endpoint counts, auth types, MCP connection details with per-tool JSON-RPC examples, and the community catalog browser.

---

## Ports & Configuration

| Surface | Port | Protocol | Notes |
|---------|------|----------|-------|
| Dashboard | 9876 | HTTP | Dark-themed web UI |
| REST API | 9876 | HTTP | `/api/*` endpoints |
| MCP server | 9876 | HTTP | JSON-RPC 2.0 at `/mcp` |
| Webhooks | 9876 | HTTP | `/hooks/<name>` trigger |
| CLI | local | — | Routes through dashboard HTTP when server is running |

**Configuration file:** `~/.musterflow/config.yaml` (created on first run):

```yaml
port: 9876
data_dir: ~/.musterflow/
default_format: table
auto_completion: true
```

**Data directory** (`~/.musterflow/` by default, overridable with `--data-dir`):

```
~/.musterflow/
├── musterflow.db          # DuckDB registry (API connections)
├── config.yaml            # User configuration
├── credentials.yaml       # Masked API credentials
└── flows/                 # Starlark workflow scripts
```

**Port auto-discovery:** if port 9876 is occupied, MusterFlow tries 9877–9886. Override with `--dashboard-addr :<port>` or the `port` config key.
