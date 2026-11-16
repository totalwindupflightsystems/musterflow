#!/usr/bin/env python3
"""Generate N distinct small OpenAPI specs for CLI-scaling probes."""
import json
import os
import sys

outdir = sys.argv[1]
n = int(sys.argv[2]) if len(sys.argv) > 2 else 40
port = 18099
os.makedirs(outdir, exist_ok=True)
for i in range(n):
    spec = {
        "openapi": "3.0.3",
        "info": {"title": "Scale API %02d" % i, "version": "1.0.%d" % i},
        "servers": [{"url": "http://127.0.0.1:%d" % port}],
        "paths": {
            "/res%02d" % i: {
                "get": {"operationId": "listRes%02dItems" % i, "summary": "list %d" % i,
                        "responses": {"200": {"description": "ok"}}},
                "post": {"operationId": "createRes%02dItem" % i, "summary": "create %d" % i,
                         "requestBody": {"required": True, "content": {
                             "application/json": {"schema": {"type": "object", "properties": {
                                 "name": {"type": "string"}}}}}},
                         "responses": {"201": {"description": "created"}}},
            },
            "/res%02d/{rid}" % i: {
                "get": {"operationId": "getRes%02dItem" % i, "summary": "get %d" % i,
                        "parameters": [{"name": "rid", "in": "path", "required": True,
                                        "schema": {"type": "integer"}}],
                        "responses": {"200": {"description": "ok"}}},
            },
        },
    }
    with open(os.path.join(outdir, "spec%02d.json" % i), "w") as f:
        json.dump(spec, f)
print("wrote %d specs to %s" % (n, outdir))
