#!/usr/bin/env python3
"""Deterministic local CRUD API for musterflow dogfood probing.

Modes:
  /items            GET (list, ?status=), POST (create)
  /items/{id}       GET, PUT, DELETE
  /openapi.json     OpenAPI 3.0 spec
  /_log             GET -> JSON list of every request seen (method, path, body)
  /_mode?flaky=1    POST-fail injection: next N POSTs /items answer 500 AFTER
                    recording the intended side effect (counter bumped), so a
                    client-side retry is observable as duplicate side effects.
"""
import json
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18099

STATE = {"next_id": 1, "items": {}, "log": [], "fail_posts": 0, "seq": 0}
LOCK = threading.Lock()

SPEC = {
    "openapi": "3.0.3",
    "info": {"title": "Dogfood Items API", "version": "1.0.0"},
    "servers": [{"url": "http://127.0.0.1:%d" % PORT}],
    "paths": {
        "/items": {
            "get": {
                "operationId": "listItems",
                "summary": "List items",
                "parameters": [
                    {"name": "status", "in": "query", "required": False,
                     "schema": {"type": "string"}},
                    {"name": "limit", "in": "query", "required": False,
                     "schema": {"type": "integer"}},
                ],
                "responses": {"200": {"description": "ok", "content": {
                    "application/json": {"schema": {
                        "type": "array", "items": {"$ref": "#/components/schemas/Item"}}}}}},
            },
            "post": {
                "operationId": "createItem",
                "summary": "Create an item",
                "requestBody": {"required": True, "content": {
                    "application/json": {"schema": {"$ref": "#/components/schemas/ItemInput"}}}},
                "responses": {"201": {"description": "created", "content": {
                    "application/json": {"schema": {"$ref": "#/components/schemas/Item"}}}}},
            },
        },
        "/items/{itemId}": {
            "get": {
                "operationId": "getItem",
                "summary": "Fetch one item",
                "parameters": [{"name": "itemId", "in": "path", "required": True,
                                "schema": {"type": "integer"}}],
                "responses": {"200": {"description": "ok", "content": {
                    "application/json": {"schema": {"$ref": "#/components/schemas/Item"}}}},
                    "404": {"description": "not found"}},
            },
            "put": {
                "operationId": "updateItem",
                "summary": "Replace an item",
                "parameters": [{"name": "itemId", "in": "path", "required": True,
                                "schema": {"type": "integer"}}],
                "requestBody": {"required": True, "content": {
                    "application/json": {"schema": {"$ref": "#/components/schemas/ItemInput"}}}},
                "responses": {"200": {"description": "ok", "content": {
                    "application/json": {"schema": {"$ref": "#/components/schemas/Item"}}}}},
            },
            "delete": {
                "operationId": "deleteItem",
                "summary": "Delete an item",
                "parameters": [{"name": "itemId", "in": "path", "required": True,
                                "schema": {"type": "integer"}}],
                "responses": {"204": {"description": "deleted"},
                              "404": {"description": "not found"}},
            },
        },
    },
    "components": {"schemas": {
        "ItemInput": {"type": "object", "required": ["name"],
                      "properties": {"name": {"type": "string"},
                                     "status": {"type": "string"},
                                     "tags": {"type": "array", "items": {"type": "string"}}}},
        "Item": {"type": "object",
                 "properties": {"id": {"type": "integer"}, "name": {"type": "string"},
                                "status": {"type": "string"},
                                "tags": {"type": "array", "items": {"type": "string"}}}},
    }},
}


class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def _send(self, code, payload=None, extra=None):
        body = b"" if payload is None else json.dumps(payload).encode()
        self.send_response(code)
        if body:
            self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        for k, v in (extra or {}).items():
            self.send_header(k, v)
        self.end_headers()
        if body:
            self.wfile.write(body)

    def _body(self):
        n = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(n) if n else b""
        try:
            return json.loads(raw) if raw else {}
        except Exception:
            return {"_unparsed": raw.decode("utf-8", "replace")}

    def _record(self, method, path, payload):
        with LOCK:
            STATE["seq"] += 1
            STATE["log"].append({"seq": STATE["seq"], "method": method,
                                 "path": path, "body": payload,
                                 "headers": {k: v for k, v in self.headers.items()
                                             if k.lower() in ("content-type", "authorization",
                                                              "x-dogfood", "idempotency-key")}})

    def do_GET(self):
        path, _, query = self.path.partition("?")
        self._record("GET", self.path, None)
        if path == "/openapi.json":
            return self._send(200, SPEC)
        if path == "/_log":
            with LOCK:
                return self._send(200, {"log": STATE["log"]})
        if path == "/_state":
            with LOCK:
                return self._send(200, {"items": STATE["items"], "seq": STATE["seq"],
                                        "fail_posts": STATE["fail_posts"]})
        if path == "/items":
            want = None
            for part in query.split("&"):
                k, _, v = part.partition("=")
                if k == "status":
                    want = v
            with LOCK:
                rows = [i for i in STATE["items"].values()
                        if want is None or i.get("status") == want]
            return self._send(200, sorted(rows, key=lambda r: r["id"]))
        if path.startswith("/items/"):
            try:
                iid = int(path.rsplit("/", 1)[1])
            except ValueError:
                return self._send(400, {"error": "bad id"})
            with LOCK:
                item = STATE["items"].get(iid)
            if not item:
                return self._send(404, {"error": "not found", "message": "Item not found"})
            return self._send(200, item)
        return self._send(404, {"error": "no route"})

    def do_POST(self):
        path, _, query = self.path.partition("?")
        payload = self._body()
        self._record("POST", self.path, payload)
        if path == "/_mode":
            n = 0
            for part in query.split("&"):
                k, _, v = part.partition("=")
                if k == "flaky":
                    n = int(v)
            with LOCK:
                STATE["fail_posts"] = n
            return self._send(200, {"fail_posts": n})
        if path == "/items":
            with LOCK:
                iid = STATE["next_id"]
                STATE["next_id"] += 1
                item = {"id": iid, "name": payload.get("name", ""),
                        "status": payload.get("status", "new"),
                        "tags": payload.get("tags", [])}
                STATE["items"][iid] = item
                fail = STATE["fail_posts"] > 0
                if fail:
                    STATE["fail_posts"] -= 1
            if fail:
                # side effect ALREADY happened; answer with a server error
                return self._send(500, {"error": "injected failure", "created_id": iid})
            return self._send(201, item)
        return self._send(404, {"error": "no route"})

    def do_PUT(self):
        path, _, _q = self.path.partition("?")
        payload = self._body()
        self._record("PUT", self.path, payload)
        if path.startswith("/items/"):
            try:
                iid = int(path.rsplit("/", 1)[1])
            except ValueError:
                return self._send(400, {"error": "bad id"})
            with LOCK:
                item = STATE["items"].get(iid)
                if not item:
                    return self._send(404, {"error": "not found"})
                item.update({"name": payload.get("name", item["name"]),
                             "status": payload.get("status", item["status"]),
                             "tags": payload.get("tags", item["tags"])})
            return self._send(200, item)
        return self._send(404, {"error": "no route"})

    def do_DELETE(self):
        path, _, _q = self.path.partition("?")
        self._record("DELETE", self.path, None)
        if path.startswith("/items/"):
            try:
                iid = int(path.rsplit("/", 1)[1])
            except ValueError:
                return self._send(400, {"error": "bad id"})
            with LOCK:
                gone = STATE["items"].pop(iid, None)
            if not gone:
                return self._send(404, {"error": "not found"})
            return self._send(204)
        return self._send(404, {"error": "no route"})


if __name__ == "__main__":
    srv = ThreadingHTTPServer(("127.0.0.1", PORT), H)
    print("listening on %d" % PORT, flush=True)
    srv.serve_forever()
