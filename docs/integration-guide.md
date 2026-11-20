# MusterFlow Integration Guide

A comprehensive developer guide covering all four surfaces of MusterFlow: the **CLI**, the **dashboard HTTP API**, the **MCP server**, and the **Starlark workflow engine** — plus the **WASM transform** commands that manage installed transform modules. Every example is pre-verified against a running MusterFlow instance.

---

## Quickstart

Build from source and connect your first API:

```bash
git clone https://github.com/totalwindupflightsystems/musterflow.git
cd musterflow
go build -o musterflow ./cmd/musterflow/

# Connect an OpenAPI spec — subcommands are generated automatically
./musterflow connect http://127.0.0.1:18099/openapi.yaml
```

Output:

```
✓ Connected: pet-store-api
  ID: 9e147e1a050203c8
  Version: 1.0.0
  Endpoints: 5
  Base URL: http://localhost:8080

Try: musterflow pet-store-api --help
```

> **Note:** The ID is generated per connection — yours will differ. The remaining output (name, version, endpoint count, base URL) is deterministic for a given spec.

Now every operation in that spec is a CLI subcommand. The connected API `pet-store-api` generates a `pets` command group with one subcommand per operationId (`list-pets`, `create-pet`, `get-pet`, `update-pet`, `delete-pet`):

```bash
$ musterflow pet-store-api pets list-pets
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

Connects an API by its OpenAPI spec URL. MusterFlow fetches the spec, parses every endpoint, and generates CLI subcommands named after the slugified spec title. The connection is persisted in DuckDB at `~/.musterflow/musterflow.db`.

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
| `musterflow refresh <id|name>` | Re-fetch and update an API's spec |
| `musterflow catalog search <q>` | Search the community catalog |
| `musterflow catalog push <id|name>` | Share an API connection |
| `musterflow catalog pull <id>` | Install a community API |
| `musterflow auth add <id>` | Add credentials (apikey, bearer, oauth2, mtls) |
| `musterflow auth list` | List configured credentials (keys masked) |
| `musterflow auth login <id>` | OAuth2 browser flow (requires `--client-id`, `--client-secret`, `--auth-url`, `--token-url`) |
| `musterflow config show` | Print active configuration |
| `musterflow transform list` | List `.wasm` transforms installed in `~/.musterflow/transforms/` |
| `musterflow transform install <entry>` | Install a transform from the catalog — placeholder, errors today (see WASM Transforms below) |
| `musterflow export` / `import` | JSONL export/import of the API registry |

### OAuth2 Login (`auth login`)

`auth login` runs the OAuth2 authorization-code flow (no PKCE) and stores the resulting access token as that API's credential. **All four of the following flags are required**: omit any one and the command fails with `Error: --client-id is required for OAuth2 login` (or the matching flag name) before a browser is opened.

```bash
musterflow auth login slack \
  --client-id <id> \
  --client-secret <secret> \
  --auth-url https://slack.com/oauth/v2/authorize \
  --token-url https://slack.com/api/oauth.v2.access
```

| Flag | Required | Default | Meaning |
|------|----------|---------|---------|
| `--client-id` | yes | — | OAuth2 client ID |
| `--client-secret` | yes | — | OAuth2 client secret |
| `--auth-url` | yes | — | Authorization endpoint the browser is sent to |
| `--token-url` | yes | — | Token endpoint the authorization code is exchanged at |
| `--scopes` | no | none | Comma-separated scopes |
| `--redirect-port` | no | `19876` | Port for the local callback server |

The command prints the auth URL, opens the browser (printing the URL for manual use if it cannot launch one), waits for the callback on the redirect port, exchanges the code, and stores the credential as `type=oauth2` in the config `auth:` map keyed by the API id (see [Ports & Configuration](#ports--configuration)). The raw token is additionally cached at `~/.musterflow/tokens/<client-id>.yaml` (mode `0600`; the cache is always written under the default `~/.musterflow`, even when `--data-dir` points elsewhere).

### Raw Request Bodies (`--body`)

Generated operation commands for POST/PUT/PATCH endpoints that take a JSON body also accept `-b, --body`. From the built binary (`musterflow swagger-petstore-openapi-3-0 pet add-pet --help`):

```
-b, --body string   Raw JSON request body (overrides individual body flags)
```

- **The value is sent verbatim and REPLACES the whole request body.** When `--body` is supplied, the per-property flags (`--name`, `--id`, …) are ignored — the payload is not merged with them, and required body properties are not enforced (the raw object is assumed to carry them). Without `--body`, the body is assembled from those per-property flags instead.
- **The value must be valid JSON.** A non-JSON value fails before any request is sent:

  ```
  $ musterflow swagger-petstore-openapi-3-0 pet add-pet --body 'not-json'
  Error: invalid JSON for --body: value is not valid JSON (value: "not-json")
  ```

  Exit status is 1.

Example — one object defines the entire body:

```bash
musterflow swagger-petstore-openapi-3-0 pet add-pet -b '{"name":"rex","id":7}'
```

---

## Dashboard HTTP API

Start the server with `musterflow start`. The dashboard, REST API, MCP endpoint, and webhooks all share port **9876** by default.

### Endpoints

```
GET  /api/health                   → 200  {"connected_apis":N,"status":"ok"}
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
GET  /api/export                    → 200  whole connection registry streamed as JSONL (application/x-ndjson)
POST /api/import                    → 200  import connections from a JSONL body → {"imported":N}
```

Export and import are a matched pair. `GET /api/export` streams one JSON object per line (`application/x-ndjson`), and `POST /api/import` consumes exactly that stream: the request body is JSONL, each parsed connection is inserted, and an entry whose `id` already exists is replaced — the response reports how many rows were imported as `{"imported":N}`. That is how connections move between hosts (backup, transfer to another machine) without opening DuckDB directly. Any other method answers 405, and an empty body is rejected with 400 `{"error":"missing request body"}`.

### JSON Response Shapes

**GET /api/apis** — list of connected APIs:

```json
{
  "apis": [
    {
      "id": "9e147e1a050203c8",
      "name": "pet-store-api",
      "spec_url": "http://127.0.0.1:18099/openapi.yaml",
      "base_url": "http://localhost:8080",
      "version": "1.0.0",
      "description": "A simple API for managing pets",
      "auth_type": "none",
      "added_at": "2026-08-18T19:48:00.549782Z",
      "updated_at": "2026-08-18T19:48:00.549782Z",
      "endpoint_count": 5
    }
  ]
}
```

> **Note:** `name` is the API slug (used in CLI subcommands), not the human-readable spec title. `auth_type` is `"none"` for unauthenticated APIs (not an empty string). `added_at` and `updated_at` are RFC 3339 timestamps generated per connection — yours will differ.

**POST /api/apis** — connect a new API (request body):

```json
{
  "spec_url": "http://127.0.0.1:18099/openapi.yaml",
  "base_url": "",
  "name": "",
  "auth_type": ""
}
```

Response (201):

```json
{
  "id": "9e147e1a050203c8",
  "name": "pet-store-api",
  "spec_title": "pet-store-api",
  "spec_version": "1.0.0",
  "endpoint_count": 5,
  "base_url": "http://localhost:8080"
}
```

> **Note:** The `id` is generated per connection — yours will differ. `spec_title` and `name` are both the slugified spec name, not the human-readable title.

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
      "source": "print(42); print(\"trigger=\" + (\"none\" if trigger == None else \"set\"))",
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
        "name": "listPets",
        "description": "List all pets",
        "inputSchema": {
          "type": "object",
          "properties": {
            "limit": { "type": "integer" },
            "offset": { "type": "integer" }
          }
        }
      },
      {
        "name": "createPet",
        "description": "Create a new pet",
        "inputSchema": {
          "type": "object",
          "properties": {
            "name": { "type": "string" },
            "species": { "type": "string" },
            "age": { "type": "integer" },
            "tags": { "type": "array" }
          },
          "required": ["name"]
        }
      },
      {
        "name": "getPet",
        "description": "Get a specific pet",
        "inputSchema": {
          "type": "object",
          "properties": {
            "petId": { "type": "string" }
          },
          "required": ["petId"]
        }
      }
    ]
  }
}
```

> **Note:** The full response includes up to ten tools — five `operationId`s, each published twice; three entries are shown above for brevity. Every operation is published as `<api-slug>__<operationId>` (e.g. `pet-store-api__listPets`), where the slug comes from the connection's name, so two APIs that both define `listPets` get two distinct tools that each call their own base URL. The bare `operationId` (e.g. `listPets`) is also published while it identifies exactly one connected API; once any second API defines the same `operationId`, the bare name is withdrawn and only the namespaced names remain.

### Call a Tool

**Request:**

```bash
curl -X POST http://localhost:9876/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"listPets","arguments":{"limit":10}}}'
```

**Response (200):**

> The response content depends on the target API's live backend. The shape is always a JSON-RPC 2.0 envelope with a `result.content` array of `{type, text}` objects:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "[{\"id\":\"1\",\"name\":\"Bella\",\"species\":\"dog\"},{\"id\":\"2\",\"name\":\"Max\",\"species\":\"cat\"}]"
      }
    ]
  }
}
```

> **Note:** The `text` field contains the target API's raw JSON response, stringified. The actual values depend on the backend serving the connected spec.

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

### HTTP Builtins

Every flow is predeclared with three HTTP builtins, implemented with the Go standard library (no extra dependencies):

| Builtin | Signature | Notes |
|---------|-----------|-------|
| `http_get` | `http_get(url, headers=None)` | GET |
| `http_post` | `http_post(url, body=None, headers=None)` | POST |
| `http_request` | `http_request(method, url, headers=None, body=None)` | Any method in the closed set below |

Argument rules:

- `url` — non-empty string.
- `method` — upper-cased before validation; must be one of `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`. Anything else raises an error that lists the valid methods.
- `headers` — optional `starlark.Dict` of `str -> str`. Non-string keys or values raise.
- `body` — optional string or bytes. A non-empty body on `GET`/`HEAD` raises.

Response dict — exactly three keys:

| Key | Type | Meaning |
|-----|------|---------|
| `status` | int | HTTP status code |
| `body` | string | response body, truncated at 1 MiB |
| `headers` | dict of str -> str | first value per canonical header name |

Semantics:

- **HTTP status codes are data, not errors.** A 404 or 500 returns a normal dict; only transport failures (DNS, dial, TLS, timeout) raise. Raised errors are prefixed with the failing builtin, e.g. `http_get: Get "http://127.0.0.1:1/": dial tcp ...`.
- **Timeout is 10s per request.**
- **Response bodies are capped at 1 MiB** and truncated past that, so an upstream cannot make the daemon buffer an unbounded body.

```python
def main():
    resp = http_get("https://httpbin.org/json")
    print("status:", resp["status"])
    print(resp["headers"]["Content-Type"])

    created = http_post("https://httpbin.org/post", body="hello",
                        headers={"Content-Type": "text/plain"})
    print("created:", created["status"])

    missing = http_request("DELETE", "https://httpbin.org/status/404")
    print("not an error:", missing["status"])

main()
```

Starlark has no top-level `if`/`for` statements, so flow logic goes inside a function body that you then call.

### Passing the Trigger Payload

The payload reaches the flow as the `trigger` global, but each entry point wraps it differently:

| Entry point | How the payload is supplied |
|-------------|-----------------------------|
| CLI | `musterflow flow run <name> --payload '{"repo":"x"}'` — the flag value is the payload object |
| Dashboard API | `POST /api/flows/<name>/run` expects the payload under the `trigger` key: `{"trigger": {"repo": "x"}}` |
| Webhook | `POST /hooks/<name>` uses the raw request body as the payload (no `trigger` wrapper) |

When no payload is supplied, `trigger` is `None`.

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

Response (200):

```json
{"result":"42\ntrigger=set"}
```

The `trigger` global inside the Starlark script receives the parsed JSON payload. The response `result` field contains the script's stdout output.

---

## WASM Transforms

A transform is a WebAssembly module that rewrites payloads — redacting PII, reshaping a response, or enriching it with derived fields. MusterFlow uses the WASI stdin/stdout convention: **the module reads JSON from stdin and writes the transformed JSON to stdout**, so a transform is a single `.wasm` file with no ABI to bind, compiled for `GOOS=wasip1 GOARCH=wasm` (or any toolchain that emits a WASI preview 1 module, e.g. Rust's `wasm32-wasip1` target).

When a transform is executed through the in-process runner (`wasm.Run` in `internal/wasm`), wazero sandboxes it with the default limits: **128 MiB max memory, 30s max execution time, no network access**.

> **What ships in this release:** the CLI exposes exactly two subcommands, `list` and `install`. `list` reports what is on disk; `install` is a catalog placeholder that exits non-zero. No CLI command, dashboard route, MCP tool, or flow builtin executes a transform yet — the runner exists in the `internal/wasm` package but is not wired to any surface. This section documents the install and inventory surface as shipped, not an end-to-end transform pipeline.

| Subcommand | Behavior today |
|------------|----------------|
| `musterflow transform list` | Reads `~/.musterflow/transforms/` and prints every `.wasm` file found |
| `musterflow transform install <catalog-entry>` | Catalog placeholder — exits 1 with "not yet implemented" |

Both subcommands take no flags of their own; only the global flags (`--data-dir`, `--dashboard-addr`, `--no-dashboard`, `--output-file`) appear in their `--help`.

### Where Transforms Live

Installed transforms are plain `.wasm` files in:

```
~/.musterflow/transforms/
```

`musterflow transform list` creates the directory if it is missing. Every file with a `.wasm` extension is listed; other files and subdirectories are ignored.

> **Note:** `~` is your home directory. The transform commands resolve this path from `$HOME` only — the global `--data-dir` flag relocates the registry (`musterflow.db`) but does **not** relocate the transform directory. To point transforms elsewhere, run the command with a different `HOME`.

### Listing Installed Transforms

```bash
musterflow transform list
```

With nothing installed, the command exits 0 and prints the directory hint:

```
$ musterflow transform list
No transforms installed.
Place .wasm files in /home/you/.musterflow/transforms/ to install.
```

> The path is `$HOME`-expanded — yours will differ.

With one transform installed:

```
$ musterflow transform list
Installed transforms:
  pii-redact.wasm (/home/you/.musterflow/transforms/pii-redact.wasm)
```

Each entry is `<file name> (<absolute path>)`. The registry lists files; it does not read metadata out of the module, so there is no description, version, or author column and no verbose variant of the command.

### Installing from the Catalog

```bash
musterflow transform install <catalog-entry>
```

The subcommand takes exactly one catalog entry id. Catalog integration is not implemented in this release, so it fails for every entry with exit code 1:

```
$ musterflow transform install pii-redact
Error: WASM transform catalog integration is not yet implemented; place .wasm files in /home/you/.musterflow/transforms to install transforms manually
```

Calling it with no argument fails earlier, during argument validation:

```
$ musterflow transform install
Error: accepts 1 arg(s), received 0
```

### Manual Install

Because `transform list` reads the directory directly, copying a module in is the install path that works today:

```bash
mkdir -p ~/.musterflow/transforms
cp ./pii-redact.wasm ~/.musterflow/transforms/
musterflow transform list
```

### Example — Build, Install, List

This module reads a JSON object from stdin, replaces `email` and `phone` with `[redacted]`, and writes the object to stdout — the whole transform contract in one file:

```bash
mkdir -p /tmp/pii-redact && cd /tmp/pii-redact

cat > main.go <<'EOF'
package main

import (
	"encoding/json"
	"io"
	"os"
)

// WASI stdin/stdout transform: read a JSON object, redact two fields, write it back.
func main() {
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(1)
	}
	var in map[string]any
	if err := json.Unmarshal(body, &in); err != nil {
		os.Exit(1)
	}
	for _, key := range []string{"email", "phone"} {
		if _, ok := in[key]; ok {
			in[key] = "[redacted]"
		}
	}
	out, err := json.Marshal(in)
	if err != nil {
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		os.Exit(1)
	}
}
EOF

go mod init pii-redact
GOOS=wasip1 GOARCH=wasm go build -o pii-redact.wasm .

mkdir -p ~/.musterflow/transforms
cp pii-redact.wasm ~/.musterflow/transforms/
musterflow transform list
```

The last command prints the installed module:

```
Installed transforms:
  pii-redact.wasm (/home/you/.musterflow/transforms/pii-redact.wasm)
```

> **Note:** `go mod init` is required even for a single file — the Go toolchain builds in module mode. The build emits a WASI preview 1 module (starting with the magic bytes `00 61 73 6d`), and that is all the registry checks: it selects files by `.wasm` extension and never loads or validates the module.

---

## Example Walkthrough — End to End

### 1. Connect an API

```bash
$ musterflow connect http://127.0.0.1:18099/openapi.yaml
✓ Connected: pet-store-api
  ID: 9e147e1a050203c8
  Version: 1.0.0
  Endpoints: 5
  Base URL: http://localhost:8080

Try: musterflow pet-store-api --help
```

> The ID is generated per connection — yours will differ.

### 2. Call it from the CLI

```bash
$ musterflow pet-store-api pets list-pets
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
{"apis":[{"id":"9e147e1a050203c8","name":"pet-store-api","spec_url":"http://127.0.0.1:18099/openapi.yaml","base_url":"http://localhost:8080","version":"1.0.0","description":"A simple API for managing pets","auth_type":"none","added_at":"2026-08-18T19:48:00.549782Z","updated_at":"2026-08-18T19:48:00.549782Z","endpoint_count":5}]}

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
data_dir: /home/<user>/.musterflow
default_format: table
auto_completion: true
```

`data_dir` is written as a resolved absolute path with no trailing slash; a `~` you set yourself is not expanded, so always pass an absolute path.

**Data directory** (`~/.musterflow/` by default, overridable with `--data-dir`):

```
~/.musterflow/
├── config.yaml            # User configuration — also holds API credentials (see below)
├── musterflow.db          # DuckDB registry (API connections)
├── flows/                 # Starlark workflow scripts + <name>.star.json metadata sidecars
├── transforms/            # WASM transforms (.wasm files, created by `transform list`)
└── tokens/                # OAuth2 token cache from `auth login` (<client-id>.yaml, mode 0600)
```

`flows/<name>.star` is the Starlark source; the `<name>.star.json` sidecar beside it carries the flow's `description` and `webhook` flag (`internal/workflow/engine.go` `writeMeta`), which `flow list` reads back. A missing sidecar is not an error — the flow reads as description-less and non-webhook, which covers flows created before sidecars existed. `transforms/` and `tokens/` resolve under the default `~/.musterflow` even when `--data-dir` points elsewhere.

**CLI writes route through the dashboard.** When a dashboard answers on the configured port the CLI does not open `musterflow.db` at all — it goes over the dashboard's HTTP API, so reads and writes coexist with the running server (see [Operational Pitfalls](#operational-pitfalls) for the lock error this avoids and the two sanctioned ways forward).

There is no separate credentials file: per-API credentials live in `config.yaml` under a top-level `auth:` map, keyed by API id:

```yaml
auth:
  <api-id>:
    type: apikey        # none, apikey, bearer, oauth2, mtls
    key: <secret>       # API key or bearer token
```

Because that file carries plaintext keys and tokens, it is written with mode `0600`. `musterflow auth list` prints the configured entries with the secret masked: keys longer than 8 characters show only their first and last four characters (`<api-id>: type=apikey key=ghp_…3456`), and keys of 8 characters or fewer print as `key=****`. `musterflow auth get <api-id>` prints the raw value.

**Port auto-discovery:** if another program already holds the configured port, MusterFlow tries the nine ports above it (`port+1` … `port+9` — 9877–9885 for the default 9876) and serves on the first free one, printing the port it chose. A port held by another MusterFlow dashboard is never bypassed: `start` refuses and tells you to reuse it. Override with `--dashboard-addr :<port>` or the `port` config key.

### Operational Pitfalls

**A running dashboard holds a single-writer lock on `<data-dir>/musterflow.db`.** DuckDB allows exactly one writing process per database file, so any second process that opens that data dir directly fails with the driver's raw text:

```
IO Error: Could not set lock on file "<data-dir>/musterflow.db": Conflicting lock is held in <path-to-musterflow> (PID <pid>) by user <user>
```

The CLI classifies that error (`internal/app/lockerr.go`), names the lock-holding process, and points at the fix instead of reporting an empty registry. Two sanctioned ways forward:

- **Let the CLI route through the running dashboard (the default).** When a dashboard answers on the configured port, CLI commands go over its HTTP API and never touch the database file — no lock, no conflict. If the dashboard is on another port, `--dashboard-addr 127.0.0.1:<port>` (or setting `port` in the config) restores routing; an explicit `--data-dir` that names the very directory the dashboard serves is also routed back through it, with a note on stderr.
- **Use a separate `--data-dir` for local/offline work.** A distinct directory has its own `musterflow.db` and therefore its own lock, so local experimentation cannot collide with the running dashboard's registry.

`--no-dashboard` against the live data dir is exactly the failing case: it forces local registry mode while the dashboard still holds the lock.
