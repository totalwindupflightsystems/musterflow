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
$ musterflow swagger-petstore-openapi-3-0 listPets --limit 5
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
# Create a new Starlark workflow
musterflow flow create --name myflow --source flow.star --description "My first flow"

# List all workflows
musterflow flow list

# Run a workflow
musterflow flow run myflow
```

A flow whose `.star` source prints output returns it from `flow run`. Flows that include an `if trigger != None:` guard receive the webhook payload as the `trigger` global.

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
GET  /health                        → 200  {"status":"ok","connected_apis":N}
GET  /                              → 200  (dashboard HTML)
GET  /api/apis                      → 200  {"apis":[...]}
POST /api/apis                      → 201  connect an OpenAPI spec
GET  /api/apis/<id>                 → 200  single API details
DELETE /api/apis/<id>               → 200  disconnect an API
POST /api/apis/<id>/refresh         → 404  not implemented (use the CLI `refresh` command)
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
      "source": "print(6 * 7)\n",
      "webhook_url": ""
    }
  ]
}
```

**POST /api/flows/<name>/run** — run result:

```json
{
  "output": "42\n"
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
        "name": "swagger-petstore-openapi-3-0__listPets",
        "description": "Lists all pets",
        "inputSchema": {
          "type": "object",
          "properties": {
            "limit": { "type": "integer" }
          }
        }
      }
    ]
  }
}
```

### Call a Tool

**Request:**

```bash
curl -X POST http://localhost:9876/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"swagger-petstore-openapi-3-0__listPets","arguments":{"limit":3}}}'
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
        "text": "[{\"id\":1,\"name\":\"Bella\",\"status\":\"sold\"},{\"id\":2,\"name\":\"Max\",\"status\":\"available\"},{\"id\":3,\"name\":\"Luna\",\"status\":\"pending\"}]"
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
$ cat flow.star
print(6 * 7)

$ musterflow flow create --name myflow --source flow.star --description "My first flow"
$ musterflow flow run myflow
42
```

Flows with `print()` output return their printed text from `flow run`.

### Webhook-Triggered Flows

A flow can receive an external webhook payload. Add a trigger guard to your `.star` file:

```python
if trigger != None:
    print("Received: " + str(trigger))
```

When this flow is created, its `webhook_url` field is populated. Non-webhook flows omit `webhook_url` entirely.

Trigger the flow via HTTP:

```bash
curl -X POST http://localhost:9876/hooks/myflow \
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
$ musterflow swagger-petstore-openapi-3-0 listPets --limit 5
```

(Formatted table output — see the README for the sample.)

### 3. Start the Dashboard

```bash
$ musterflow start
```

Dashboard is now serving at `http://localhost:9876`. In another terminal:

```bash
# Health check
$ curl http://localhost:9876/health
{"status":"ok","connected_apis":1}

# List connected APIs
$ curl http://localhost:9876/api/apis
{"apis":[...]}

# List MCP tools
$ curl -X POST http://localhost:9876/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

### 4. Create and Run a Workflow

```bash
$ echo 'print(6 * 7)' > flow.star
$ musterflow flow create --name myflow --source flow.star --description "My first flow"
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
