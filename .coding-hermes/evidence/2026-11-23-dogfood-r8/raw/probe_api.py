#!/usr/bin/env python3
"""Probe OpenAPI CRUD backend for musterflow dogfood round 8.

Serves an OpenAPI 3.0 spec at /openapi.json plus a small CRUD API it
describes (GET/POST /pets, GET /pets/{id}, GET /state). Also exposes
/echo (returns the request as JSON) and optional /slow?ms=N for latency
checks. One purpose per request; every transaction logged to stdout.
"""
import json
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PETS = {"1": {"id": 1, "name": "probe-cat", "status": "available"},
        "2": {"id": 2, "name": "probe-dog", "status": "sold"}}
NEXT_ID = 3

SPEC = {
    "openapi": "3.0.0",
    "info": {"title": "probe-pets", "version": "1.0.8"},
    "servers": [{"url": "http://127.0.0.1:20081"}],
    "paths": {
        "/pets": {
            "get": {
                "summary": "list pets",
                "operationId": "find-pets",
                "parameters": [{"name": "status", "in": "query",
                                "schema": {"type": "string"}, "required": False}],
                "responses": {"200": {"description": "ok"}},
            },
            "post": {
                "summary": "create pet",
                "operationId": "create-pet",
                "requestBody": {"required": True, "content": {
                    "application/json": {"schema": {"$ref": "#/components/schemas/Pet"}}}},
                "responses": {"201": {"description": "created"}},
            },
        },
        "/pets/{id}": {
            "get": {
                "summary": "get pet by id",
                "operationId": "get-pet",
                "parameters": [{"name": "id", "in": "path", "required": True,
                                "schema": {"type": "string"}}],
                "responses": {"200": {"description": "ok"}, "404": {"description": "missing"}},
            },
        },
    },
    "components": {"schemas": {"Pet": {
        "type": "object", "required": ["name"],
        "properties": {"name": {"type": "string"}, "status": {"type": "string"}}}}},
}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"

    def _send(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        global NEXT_ID
        path, _, query = self.path.partition("?")
        q = dict(p.split("=", 1) for p in query.split("&") if "=" in p)
        time.sleep(min(int(q.get("ms", 0)), 10000) / 1000.0)
        if path == "/openapi.json":
            self._send(200, SPEC)
        elif path == "/pets":
            status = q.get("status")
            pets = list(PETS.values())
            if status:
                pets = [p for p in pets if p["status"] == status]
            self._send(200, pets)
        elif path.startswith("/pets/"):
            pid = path[len("/pets/"):]
            if pid in PETS:
                self._send(200, PETS[pid])
            else:
                self._send(404, {"error": "pet not found"})
        elif path == "/state":
            self._send(200, {"pets": len(PETS), "next_id": NEXT_ID})
        elif path == "/echo":
            self._send(200, {"path": self.path})
        else:
            self._send(404, {"error": "no such path"})

    def do_POST(self):
        global NEXT_ID
        path, _, query = self.path.partition("?")
        q = dict(p.split("=", 1) for p in query.split("&") if "=" in p)
        delay = int(q.get("ms", 0))
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n)
        try:
            data = json.loads(body.decode()) if body else {}
        except ValueError:
            self._send(400, {"error": "bad json"})
            return
        if path == "/pets":
            if not data.get("name"):
                self._send(422, {"error": "name required"})
                return
            pet = {"id": NEXT_ID, "name": data["name"],
                   "status": data.get("status", "available")}
            NEXT_ID += 1
            PETS[str(pet["id"])] = pet
            self._send(201, pet)
        else:
            self._send(404, {"error": "no such path"})

    def log_message(self, fmt, *args):
        print("[%s] %s" % (self.command, fmt % args), flush=True)


if __name__ == "__main__":
    print("probe-pets listening 127.0.0.1:20081", flush=True)
    ThreadingHTTPServer(("127.0.0.1", 20081), Handler).serve_forever()
