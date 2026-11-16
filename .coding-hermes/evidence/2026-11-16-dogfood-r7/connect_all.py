#!/usr/bin/env python3
"""Connect N specs one by one through the musterflow CLI, reporting failures."""
import subprocess
import sys

mf = sys.argv[1]
datadir = sys.argv[2]
specdir = sys.argv[3]
count = int(sys.argv[4]) if len(sys.argv) > 4 else 40

ok = 0
fails = []
for i in range(count):
    spec = "%s/spec%02d.json" % (specdir, i)
    p = subprocess.run([mf, "--data-dir", datadir, "connect", spec],
                       capture_output=True, text=True, timeout=120)
    if p.returncode == 0:
        ok += 1
    else:
        fails.append((i, p.returncode, (p.stderr or p.stdout).strip()[:200]))
print("connected ok=%d fail=%d" % (ok, len(fails)))
for f in fails[:10]:
    print("FAIL", f)
