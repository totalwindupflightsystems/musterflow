# flows/ — shipped Starlark workflows

`musterflow` runs Starlark flows from `~/.musterflow/flows/`. The files here are
reference flows that ship with the repo; install one by copying it (and its
sidecar) into that directory, or by creating it through the CLI.

## consensus-e2e

Drives a live Consensus instance (the `consensus` agent harness, served over
`/api/v1`) end to end:

```
create session -> post a user message -> poll until the session settles -> assert the assistant reply is present
```

### Install

```bash
cp flows/consensus-e2e.star flows/consensus-e2e.star.json ~/.musterflow/flows/

# or, equivalently, through the CLI (the sidecar is written for you):
musterflow flow create --name consensus-e2e \
    --source "$(cat flows/consensus-e2e.star)" \
    --description "Drive a live Consensus instance end to end"
```

### Run

CLI — the payload object is passed as-is:

```bash
musterflow flow run consensus-e2e \
    --payload '{"base_url":"http://127.0.0.1:8090","api_key":"cs_ak_..."}'
```

Dashboard — the same payload, wrapped under the `trigger` key:

```bash
curl -s -X POST http://localhost:9876/api/flows/consensus-e2e/run \
    -H 'Content-Type: application/json' \
    -d '{"trigger":{"base_url":"http://127.0.0.1:8090","api_key":"cs_ak_..."}}'
```

A green run ends with the single line:

```
CONSENSUS_E2E_OK session=<id> status=idle polls=<n>
```

`api_key` is optional: omit it when the Consensus server does not require auth
(the flow then sends no `Authorization` header). See `docs/API.md` in the
consensus repo for creating an admin key.

### Trigger keys

| key | default | meaning |
|-----|---------|---------|
| `base_url` | `http://127.0.0.1:8090` | Consensus base URL |
| `api_key` | *(none)* | Bearer token |
| `agent_name` | `musterflow-e2e` | agent name stored on the session |
| `goal` | *(built-in text)* | session goal |
| `prompt` | "Reply with exactly this token…: `<nonce>`" | user message posted |
| `expect` | the nonce | substring the assistant reply must contain; `""` disables the check |
| `session_id` | *(none)* | poll this existing session instead of creating one |
| `post` | `true` | `false` polls without posting |
| `timeout_seconds` | `120` | overall poll budget |
| `poll_interval` | `2` | seconds between polls |
| `verbose` | `false` | print every poll |

### The assertion is the deliverable

The flow raises — non-zero exit from the CLI, and a non-2xx response from the
dashboard whose body carries the message (this dashboard build answers 404 for
a failed run) — when:

- the session never settles inside `timeout_seconds`,
- the session settles as `failed` / `paused` / `cancelled`,
- `last_message` is null or empty (no assistant reply),
- the reply does not contain `expect`.

A run that only proves the HTTP calls returned 200 is not a proof, so the
assertion is on the reply's content, not on the status codes.

The default `expect` token is derived from the session id the server just
minted, so a canned or pre-existing reply cannot satisfy it.

### Falsification arms

Two ways to show the assertion is not tautological:

```bash
# (a) poll a session in which no message was posted -> "no assistant reply"
musterflow flow run consensus-e2e \
    --payload '{"base_url":"http://127.0.0.1:8090","api_key":"cs_ak_...",
                "session_id":"<any-session-id>","post":false}'

# (b) require a token that cannot appear in the reply -> "does not contain the expected token"
musterflow flow run consensus-e2e \
    --payload '{"base_url":"http://127.0.0.1:8090","api_key":"cs_ak_...",
                "expect":"EXPECTED_TOKEN_THAT_NEVER_APPEARS"}'
```

Both exit non-zero. `internal/workflow/consensus_flow_test.go` proves the same
arms deterministically against a Consensus-shaped `httptest` server.

## Builtins available to flows

Every flow is predeclared with the HTTP builtins, a `sleep` builtin and the
standard `json` module:

| global | shape |
|--------|-------|
| `http_get` | `http_get(url, headers=None)` |
| `http_post` | `http_post(url, body=None, headers=None)` |
| `http_request` | `http_request(method, url, headers=None, body=None)` |
| `sleep` | `sleep(seconds)` — blocks; capped at 30s |
| `json` | `json.encode(value)`, `json.decode(string)`, … |

`sleep` is what makes a polling flow possible at all: Starlark has no clock, and
a poll loop without a delay hammers the upstream (Consensus rate-limits session
routes at 100/min). `json` is what lets a flow assert on a decoded FIELD rather
than substring-matching raw JSON.
