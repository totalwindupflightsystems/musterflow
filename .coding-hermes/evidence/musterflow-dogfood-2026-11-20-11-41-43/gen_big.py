#!/usr/bin/env python3
"""Generate a large synthetic vendor spec from clean.yaml: same operations,
inflated with N extra paths + big inline schemas, server pointed at the local
mock API. Used to measure per-invocation spec parse cost at constant operation."""
import sys, yaml, copy, json

src, dst, n = sys.argv[1], sys.argv[2], int(sys.argv[3])
doc = yaml.safe_load(open(src))

doc["info"]["title"] = "Big Inventory API"
doc["info"]["version"] = "9.9.9"
doc["servers"] = [{"url": "http://127.0.0.1:18099"}]

blob = {f"f{i}": {"type": "string", "description": "field %d padding %s" % (i, "x" * 60)} for i in range(40)}
paths = doc.setdefault("paths", {})
for i in range(n):
    paths[f"/gen/{i}"] = {
        "get": {
            "operationId": f"op{i}",
            "summary": f"generated operation {i}",
            "description": "synthetic padding " + ("y" * 400),
            "responses": {"200": {"description": "ok", "content": {"application/json": {
                "schema": {"type": "object", "properties": copy.deepcopy(blob)}}}}},
        }
    }
with open(dst, "w") as fh:
    yaml.safe_dump(doc, fh, sort_keys=False)
print("wrote", dst, "paths=", len(paths))
