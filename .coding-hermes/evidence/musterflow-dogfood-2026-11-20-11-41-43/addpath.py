#!/usr/bin/env python3
"""Append a GET /warehouse path to a spec copy (refresh reachability probe)."""
import sys, yaml

p = sys.argv[1]
with open(p) as f:
    doc = yaml.safe_load(f)
doc["paths"]["/warehouse"] = {
    "get": {
        "operationId": "getWarehouse",
        "summary": "Warehouse info (added AFTER connect)",
        "responses": {
            "200": {
                "description": "ok",
                "content": {"application/json": {"schema": {"type": "object"}}},
            }
        },
    }
}
with open(p, "w") as f:
    yaml.safe_dump(doc, f, sort_keys=False)
print("addpath: /warehouse appended")
