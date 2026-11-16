#!/usr/bin/env python3
"""Copy this round's probe scripts + captured outputs into the repo evidence dir."""
import json
import os
import shutil

src = "/home/kara/.hermes/cache/scratch/mf-1816"
dst = "/home/kara/musterflow/.coding-hermes/evidence/2026-11-16-dogfood-r7"
os.makedirs(dst, exist_ok=True)

files = [
    "echo_api.py", "blackhole.py", "t2fs.py", "gen_specs.py", "connect_all.py",
    "verify_auth_bytes.py",
    "bench-cli.json", "bench-connect-warm.json", "bench-scale-2apis.json",
    "bench-scale-42apis.json",
    "mcp_tools.json", "mcp_call_list.json", "mcp_create2.json", "tools43.json",
    "bh.err", "si.err", "tc.err", "authcall.err", "lk.err", "404.err",
    "health.txt", "spec.json",
]
copied = []
missing = []
for f in files:
    s = os.path.join(src, f)
    if os.path.exists(s):
        shutil.copy2(s, os.path.join(dst, f))
        copied.append((f, os.path.getsize(s)))
    else:
        missing.append(f)

manifest = {
    "tick": "musterflow-dogfood-2026-11-16-18-27-37",
    "head": "80119d1",
    "copied": sorted(c for c, _ in copied),
    "missing": missing,
    "sizes": {c: n for c, n in copied},
    "note": "probe scripts + raw captures from round 7; no credentials or real user data",
}
with open(os.path.join(dst, "MANIFEST.json"), "w") as fh:
    json.dump(manifest, fh, indent=1, sort_keys=True)
print("copied", len(copied), "missing", missing)
print("dir files", len(os.listdir(dst)))
