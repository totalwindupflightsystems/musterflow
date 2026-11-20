#!/usr/bin/env python3
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

ITEMS = [
    {"sku": "A-100", "qty": 41, "tags": ["fast"], "warehouse": {"code": "MAD-1"}},
    {"sku": "B-200", "qty": 7, "tags": [], "warehouse": {"code": "BCN-2"}},
    {"sku": "C-300", "qty": 0, "tags": ["cold"], "warehouse": {"code": "MAD-1"}},
]


class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _send(self, code, body):
        raw = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        p = self.path.split("?")[0]
        if p == "/items":
            self._send(200, ITEMS)
        elif p == "/stock-levels":
            self._send(200, {"total": sum(i["qty"] for i in ITEMS), "asOf": "2026-11-20T11:45:00Z"})
        elif p.startswith("/items/"):
            sku = p.rsplit("/", 1)[-1]
            for it in ITEMS:
                if it["sku"] == sku:
                    return self._send(200, it)
            self._send(404, {"error": "not found"})
        else:
            self._send(404, {"error": "no route"})

    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(n) or b"{}")
        body.setdefault("qty", 0)
        self._send(201, body)


HTTPServer(("127.0.0.1", 18099), H).serve_forever()
