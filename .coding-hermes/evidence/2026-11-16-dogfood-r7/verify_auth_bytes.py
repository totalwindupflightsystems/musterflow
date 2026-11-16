#!/usr/bin/env python3
"""Verify, without printing secrets, whether config.yaml stores the FULL credential.

Prints only lengths and sha256 prefixes. Nothing secret reaches stdout.
"""
import hashlib
import re
import sys

path = sys.argv[1]
candidates = sys.argv[2:]

text = open(path).read()
vals = {}
cur = None
for line in text.splitlines():
    m = re.match(r"^\s{4}([0-9a-f]{16}):\s*$", line)
    if m:
        cur = m.group(1)
        continue
    m2 = re.match(r"^\s{8}key:\s*(.*)$", line)
    if m2 and cur:
        vals[cur] = m2.group(1).strip()

print("auth entries:", len(vals))
for k, v in vals.items():
    print("  id=%s stored_len=%d sha=%s" % (k, len(v), hashlib.sha256(v.encode()).hexdigest()[:16]))
print("candidates:")
for c in candidates:
    h = hashlib.sha256(c.encode()).hexdigest()[:16]
    match = [k for k, v in vals.items() if hashlib.sha256(v.encode()).hexdigest()[:16] == h]
    print("  len=%d sha=%s full_match=%s" % (len(c), h, ",".join(match) or "NONE"))
