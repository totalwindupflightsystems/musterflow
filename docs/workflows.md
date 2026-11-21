# Workflow Authoring Guide

MusterFlow runs workflows ("flows") written in [Starlark](https://github.com/bazelbuild/starlark/blob/master/spec.md),
a deterministic Python-like language. A flow is a single `.star` file that MusterFlow compiles and
executes on demand: from the CLI (`musterflow flow run`), from the dashboard API
(`POST /api/flows/<name>/run`), or from an inbound webhook (`POST /hooks/<name>`).

Everything a flow *does* is visible in one place: the values it passes to `print()`. MusterFlow
captures that output and returns it as the flow's result string.

This guide is the authoring reference: storage layout, the execution model, every predeclared
builtin with its exact signature and failure modes, three complete runnable examples, and the
rules that trip people up. For the wider API surface (CLI, dashboard, MCP) see
[integration-guide.md](integration-guide.md).

---

## Where flows live

A flow is a file named `<name>.star` inside the flows directory of the data dir:

```
<data-dir>/flows/<name>.star          # Starlark source (the only file you must have)
<data-dir>/flows/<name>.star.json     # optional metadata sidecar, written by `flow create`
```

The default data dir is `~/.musterflow`; every command accepts the global `--data-dir` flag, so a
scratch instance is `musterflow --data-dir /tmp/mf scratch flow ...`.

The sidecar holds only presentation metadata:

```json
{"description":"GET JSON and decode it","webhook":true}
```

`musterflow flow list` reads the sidecar to show the description and to advertise a webhook URL.
A `.star` file with no sidecar is a perfectly runnable flow — it just has no description and is not
advertised as a webhook (see [Webhook flag is cosmetic](#webhook-flag-is-cosmetic) below). Writing
the `.star` file directly, by hand or from your editor, is supported; nothing else is required.

A flow's **name is its file name** (minus `.star`). Renaming the file renames the flow.

---

## Create, list, run

```bash
# Write the source to a file first, then create the flow from it
cat > /tmp/hello.star <<'EOF'
def main():
    print("hello from", "musterflow")

main()
EOF

musterflow --data-dir /tmp/mf-hello flow create hello --source "$(cat /tmp/hello.star)" \
  --description "Smallest useful flow"

# List
musterflow --data-dir /tmp/mf-hello flow list

# Run
musterflow --data-dir /tmp/mf-hello flow run hello
```

Verified transcript:

```
$ musterflow --data-dir /tmp/mf-hello flow create hello --source "$(cat /tmp/hello.star)" --description "Smallest useful flow"
✓ Created flow "hello"
  Description: Smallest useful flow
  Edit: /tmp/mf-hello/flows/hello.star
$ musterflow --data-dir /tmp/mf-hello flow run hello
hello from musterflow
```

Notes:

- `--source` takes **inline Starlark source text**, not a path. `--source "$(cat file.star)"` is the
  ergonomic form; a bare path is stored verbatim as the flow's source and fails when run:
  `Error: parse flow barepath: barepath.star:1:1: got '.', want primary expression`.
- Omitting `--source` writes MusterFlow's built-in starter template (a `def main():` that prints
  `Hello from flow`), so `flow create` alone always produces a runnable flow.
- `flow create` **overwrites** an existing `flows/<name>.star` and its sidecar — it is the update
  path as well as the create path.
- The name may be positional (`flow create hello`) or `--name hello`; giving both with different
  values is an error (`conflicting flow names`). Same rule for `flow run`.
- There is no `flow delete`; remove the file (`rm <data-dir>/flows/<name>.star*`) to remove a flow.
- If a dashboard is running on the configured port, the CLI routes `flow` commands through its HTTP
  API instead of touching the data dir directly. Output and exit codes are the same.

`flow run` exit status:

| Situation | Exit | stdout | stderr |
|---|---|---|---|
| flow runs to completion | 0 | captured `print()` output | — |
| compile or runtime error | 1 | *nothing* | `Error: exec flow <name>: ...` |
| unknown flow | 1 | *nothing* | `Error: read flow <name>: open ...: no such file or directory` |
| `--payload` is not a JSON object | 1 | *nothing* | `Error: parse --payload: json: cannot unmarshal array into Go value of type map[string]interface {}` |

**On error the partial output is discarded.** A flow that prints three lines and then hits a
transport failure returns no result at all — only the error. If you need diagnostics from a failing
run, print them on a flow that branches on status instead of relying on output before the failure.
The prefix names the stage that failed: `parse flow` (syntax), `compile flow` (undefined name, or a
statement that is illegal at the top level) and `exec flow` (a builtin raised at run time).

---

## HTTP entry points

### Webhook: `POST /hooks/<name>`

The raw request body is parsed as the trigger payload (a JSON object). The response is
`{"result": "<captured output>"}`.

```bash
# with a JSON body
curl -sS -X POST http://localhost:9876/hooks/webhook-echo \
  -H 'Content-Type: application/json' \
  -d '{"event":"push","repo":"musterflow"}'
# -> {"result":"event: push\nrepo: musterflow"}

# with no body
curl -sS -X POST http://localhost:9876/hooks/webhook-echo
# -> {"result":"no trigger payload; nothing to do"}

# a body that is not a JSON object is not an error: trigger is None
curl -sS -X POST http://localhost:9876/hooks/webhook-echo -d '[1,2,3]'
# -> {"result":"no trigger payload; nothing to do"}

# unknown flow
curl -sS -w ' http=%{http_code}\n' -X POST http://localhost:9876/hooks/nope
# -> {"error":"read flow nope: open .../flows/nope.star: no such file or directory"} http=404
```

Body decoding is best-effort: an empty body, malformed JSON, or a JSON value that is not an object
all end up as `trigger = None`. There is no 400 for a bad webhook body.

### Dashboard API: `POST /api/flows/<name>/run`

Same execution, different payload wrapper — the payload goes under a `trigger` key:

```bash
curl -sS -X POST http://localhost:9876/api/flows/webhook-echo/run \
  -H 'Content-Type: application/json' \
  -d '{"trigger":{"event":"deploy","repo":"r1"}}'
# -> {"result":"event: deploy\nrepo: r1"}

curl -sS -X POST http://localhost:9876/api/flows/webhook-echo/run
# -> {"result":"no trigger payload; nothing to do"}
```

Status codes: `200` on success, `400` for a malformed JSON body (`invalid JSON body`), `405` for a
non-POST method, and `404` for **both** an unknown flow and any compile/runtime error — the body's
`error` string is what distinguishes them. The other flow endpoints (`GET /api/flows`,
`POST /api/flows`) are documented in [integration-guide.md](integration-guide.md#dashboard-http-api).

---

## Execution model

A `.star` file is compiled as one Starlark module and executed top to bottom. Four things make up
the contract:

1. **Top-level statements are restricted.** Assignments, `def`s and expression statements are legal
   at the top level; `if`, `for`, `while` and `return` are not. This is Starlark, not Python:

   ```
   $ musterflow flow run loop-top
   Error: compile flow loop-top: loop-top.star:4:1: for loop not within a function
   $ musterflow flow run if-top
   Error: compile flow if-top: if-top.star:1:1: if statement not within a function
   ```

   The convention is to put the body in `def main():` and call `main()` as the last line:

   ```python
   def main():
       if trigger != None:
           print("payload:", trigger)

   main()
   ```

   A top-level *expression* is fine — `print(6 * 7)` is a valid one-line flow — but anything with
   control flow belongs in a function.

2. **`trigger` is a predeclared global.** It is a dict for a webhook/dashboard payload and `None`
   when no payload was supplied, so the guard is `if trigger != None:` (inside a function). Nested
   objects and arrays arrive as dicts and lists; scalars as their Starlark equivalents; values of a
   type Starlark cannot represent arrive as their string form.

3. **`print()` is the return channel.** Each `print()` call appends one line (arguments joined by a
   space, like Python) to the captured output. At the end, trailing newlines are trimmed and the
   result is returned as a string. Nothing else is returned — a flow with no `print()` returns `""`.

4. **The engine predeclares five globals**: `http_get`, `http_post`, `http_request`, `sleep` and
   `json`. There is no import system, no filesystem access, no environment, no clock. Any other
   name is a compile error:

   ```
   $ musterflow flow run unknown-global
   Error: compile flow unknown-global: unknown-global.star:2:5: undefined: fetch
   ```

Starlark also has no `try`/`except`: errors raised by builtins abort the flow. Error handling is
therefore *branching on the returned values*, not catching exceptions (see
[example c](#c-error-handling)).

---

## Builtin reference

The complete predeclared surface, exactly as `flowBuiltins()` installs it:

| Builtin | Signature | Returns |
|---|---|---|
| `http_get` | `http_get(url, headers=None)` | response dict (GET) |
| `http_post` | `http_post(url, body=None, headers=None)` | response dict (POST) |
| `http_request` | `http_request(method, url, headers=None, body=None)` | response dict |
| `sleep` | `sleep(seconds)` | `None` |
| `json` | `json.encode(x)`, `json.encode_indent(x, prefix="", indent="\t")`, `json.decode(x, default=<unset>)`, `json.indent(x, prefix="", indent="\t")` | string / decoded value |

Argument rules shared by the HTTP builtins:

- `url` — non-empty string; whitespace-only is rejected (`http_get: url must be a non-empty string`).
- `method` — `http_request` only; trimmed and upper-cased, then must be one of
  `GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS`. Anything else raises
  `http_request: invalid method "TRACE": valid methods are GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS`.
- `headers` — optional dict of `str -> str`. A non-dict raises
  `http_get: headers must be a dict of str to str, got list`; non-string keys/values raise too.
- `body` — optional string or bytes (`None` and `""` mean "no body"). A non-empty body on `GET` or
  `HEAD` raises `http_request: GET requests must not include a body`. Anything else raises
  `http_post: body must be a string or bytes, got int`.

### Response dict

Successful requests (any HTTP status) return a dict with exactly three keys:

| Key | Type | Meaning |
|---|---|---|
| `status` | int | HTTP status code |
| `body` | string | response body, **truncated to 1 MiB (1,048,576 bytes)** |
| `headers` | dict of `str -> str` | **first** value per canonical header name |

- **Non-2xx is data, not an error.** `404`, `500`, `501` all come back as a normal dict; branch on
  `resp["status"]`.
- **Transport failures raise**: DNS, dial, TLS, and the 10-second per-request timeout abort the
  flow with an error prefixed by the builtin, e.g.
  `http_get: Get "http://127.0.0.1:1/": dial tcp 127.0.0.1:1: connect: connection refused`.
- **The body cap is silent**: a 1.5 MB response yields `len(resp["body"]) == 1048576` with no flag.
  If you need to know whether it was truncated, ask for `Content-Length` and compare.

### `sleep(seconds)`

Blocks the flow. `seconds` is an int or a float, and **one call may not exceed 30 seconds**
(`flowMaxSleep`); it returns `None`. Every rejection is an error:

```
sleep(-1)          -> sleep: seconds must not be negative, got -1
sleep(31)          -> sleep: seconds must be at most 30s, got 31
sleep("5")         -> sleep: seconds must be a number, got string
sleep(True)        -> sleep: seconds must be a number, got bool
sleep(float("inf")) -> sleep: seconds must not be negative, got +inf
```

Polling loops are written as a bounded `for` loop inside a function, calling `sleep(...)` between
attempts. Because a run is executed inline on the dashboard's request path, keep total flow time
small (a handful of sleeps, not minutes).

### `json`

The stock `go.starlark.net/lib/json` module:

| Call | Behaviour |
|---|---|
| `json.encode(x)` | compact JSON text of a Starlark value |
| `json.encode_indent(x, prefix="", indent="\t")` | indented JSON text |
| `json.decode(s)` | parses JSON text into dicts/lists/scalars; raises on invalid input |
| `json.decode(s, default)` | same, but returns `default` instead of raising on invalid JSON |
| `json.indent(s, prefix="", indent="\t")` | re-indents JSON **text** |

Verified:

```python
def main():
    print("encode:", json.encode({"b": 2, "a": [1, 2]}))          # {"a":[1,2],"b":2}
    print("indent:", json.indent(json.encode({"b": 2})))          # multi-line text
    print(json.decode("not json", {"ok": False}))                 # {"ok": False}

main()
```

Invalid JSON without a default raises `json.decode: at offset 0, unexpected character 'n'`.

---

## Worked examples

Each example is a complete, runnable file. Examples a and c use a local fixture so they work
offline:

```bash
mkdir -p /tmp/mf-fixture
cat > /tmp/mf-fixture/index.json <<'EOF'
{"service": "catalog-api", "version": 3, "items": ["alpha", "beta", "gamma"]}
EOF
python3 -m http.server 19880 --bind 127.0.0.1 --directory /tmp/mf-fixture
```

`http://127.0.0.1:19880/index.json` now returns that document, and any other path returns 404.

### a) HTTP + JSON decode

`fetch-json.star`:

```python
def main():
    resp = http_get("http://127.0.0.1:19880/index.json")
    if resp["status"] != 200:
        print("unexpected status:", resp["status"])
        return
    data = json.decode(resp["body"])
    print("service:", data["service"])
    print("version:", data["version"])
    print("items:", len(data["items"]), ",".join(data["items"]))

main()
```

```bash
musterflow --data-dir /tmp/mf-docs flow create fetch-json --source "$(cat fetch-json.star)" \
  --description "GET JSON and decode it"
musterflow --data-dir /tmp/mf-docs flow run fetch-json
```

Verified output:

```
service: catalog-api
version: 3
items: 3 alpha,beta,gamma
```

### b) Trigger payload from a webhook

`webhook-echo.star`:

```python
def main():
    if trigger == None:
        print("no trigger payload; nothing to do")
        return
    print("event:", trigger.get("event", "unknown"))
    print("repo:", trigger.get("repo", "unknown"))

main()
```

```bash
musterflow --data-dir /tmp/mf-docs flow create webhook-echo --source "$(cat webhook-echo.star)" --webhook
musterflow --data-dir /tmp/mf-docs start --dashboard-addr :19881   # in another shell

curl -sS -X POST http://127.0.0.1:19881/hooks/webhook-echo \
  -H 'Content-Type: application/json' \
  -d '{"event":"push","repo":"musterflow"}'
```

Verified output:

```
{"result":"event: push\nrepo: musterflow"}
```

Running the same flow with no payload (`musterflow flow run webhook-echo`, or `curl -X POST
.../hooks/webhook-echo` with an empty body) prints `no trigger payload; nothing to do`, because
`trigger` is `None`.

`dict.get(key, default)` is the tolerant accessor: a payload that arrives without the expected keys
prints `unknown` rather than raising.

### c) Error handling

Two distinct failures, two distinct patterns — a non-2xx **status** is data, a **transport**
failure is an error you cannot catch:

`status-check.star`:

```python
def main():
    resp = http_get("http://127.0.0.1:19880/missing.json")
    print("get status:", resp["status"])
    if resp["status"] == 200:
        print("service:", json.decode(resp["body"])["service"])
    else:
        print("head of body:", resp["body"][:40])

    created = http_post("http://127.0.0.1:19880/items",
                        body=json.encode({"name": "delta"}),
                        headers={"Content-Type": "application/json"})
    print("post status:", created["status"])

main()
```

Verified output against the fixture (its 404 page for GET, and `501 Unsupported method` for POST
because `http.server` has no POST handler):

```
get status: 404
head of body: <!DOCTYPE HTML>
<html lang="en">
    <he
post status: 501
```

The flow completes with exit 0: both failures were *statuses*, and the flow chose what to do with
them. Note the 40-character slice — printing a whole HTML error page into a webhook response is
rarely what you want.

A transport failure is different. This flow:

```python
def main():
    print("before")
    resp = http_get("http://127.0.0.1:1/")
    print("after:", resp["status"])

main()
```

exits 1 and returns **no output at all**, not even `before`:

```
Error: exec flow transport-fail: http_get: Get "http://127.0.0.1:1/": dial tcp 127.0.0.1:1: connect: connection refused
```

Since Starlark has no `try`/`except`, there is no way to catch that in a flow. The practical rule:
anything you want to survive a failure must be checked as a **status** before it becomes a transport
error — validate configuration and inputs as data, keep each request simple, and treat a failing
flow's error string as the diagnostic.

---

## Rules and gotchas

- **Flow bodies live in functions.** `if`/`for`/`while`/`return` at the top level fail to compile
  (`for loop not within a function`, `if statement not within a function`). Write `def main():` and
  call it. Top-level assignments, `def`s and expression statements (including `print(...)`) are fine.
- **`trigger` is a predeclared global, not an argument.** It is a dict for a payload and `None`
  when there is none; guard with `if trigger != None:`. Use `trigger.get("k", default)` for
  optional keys.
- **`print()` is the only return channel.** Result = captured print lines, trailing newlines
  trimmed. A flow that computes something but prints nothing returns `""`.
- **Status codes are data; transport failures are errors.** Branch on `resp["status"]`; a DNS/dial/
  TLS/timeout failure aborts the flow.
- **One HTTP request gets 10 seconds**, response bodies are **capped at 1 MiB** (silently), and
  **one `sleep()` may not exceed 30 seconds**.
- **No `try`/`except`, no imports, no filesystem, no clock.** Only the five predeclared globals
  (`http_get`, `http_post`, `http_request`, `sleep`, `json`) plus Starlark's standard builtins
  (`len`, `range`, `str`, `sorted`, dict/list/string methods, …).
- **On failure the partial output is lost.** `flow run` prints only `Error: ...` and exits 1; the
  dashboard/webhook endpoints return `{"error": ...}` with HTTP 404.
- **Webhook flag is cosmetic.** `--webhook` only makes `flow list` and the API advertise
  `webhook_url`; `POST /hooks/<name>` runs **any** existing flow, flagged or not. Conversely a flow
  with no sidecar still runs, it just does not advertise the URL.
- **`--source` is inline source, not a file path.** Use `--source "$(cat myflow.star)"` to create
  from a file, or just write `<data-dir>/flows/<name>.star` directly.
- **`flow create` overwrites**, including the sidecar. Re-running it is how you update a flow from
  the CLI; editing the `.star` file in place is the other way.
- **The name is the file name.** `flow run hello` reads `flows/hello.star`; keep names
  filesystem-safe (letters, digits, `-`, `_`).
- **`--payload` must be a JSON object.** Arrays and scalars are rejected by the CLI parser.
- **Flow runs are inline.** They execute synchronously inside the request that triggered them
  (CLI call, dashboard request, webhook); keep them short and bounded.
