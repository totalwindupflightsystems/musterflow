#!/usr/bin/env python3
"""Measure time-to-first-success: pristine data dir -> connect -> first real command.

Each iteration uses a UNIQUE data dir (never deletes anything). Prints per-run
seconds plus the median, and the loadavg at start/end.
"""
import os
import statistics
import subprocess
import sys
import time

mf = sys.argv[1]
root = sys.argv[2]
spec = sys.argv[3]
runs = int(sys.argv[4]) if len(sys.argv) > 4 else 5
tag = sys.argv[5] if len(sys.argv) > 5 else "t2fs"

print("loadavg_start", open("/proc/loadavg").read().strip())
rows = []
for i in range(runs):
    d = os.path.join(root, "%s-run%02d" % (tag, i))
    os.makedirs(d, exist_ok=True)
    home = os.path.join(d, "home")
    os.makedirs(home, exist_ok=True)
    env = dict(os.environ, HOME=home)
    t0 = time.monotonic()
    p1 = subprocess.run([mf, "--data-dir", d, "connect", spec],
                        capture_output=True, text=True, env=env)
    t1 = time.monotonic()
    p2 = subprocess.run([mf, "--data-dir", d, "dogfood-items-api", "items", "list-items",
                         "--output", "json"], capture_output=True, text=True, env=env)
    t2 = time.monotonic()
    rows.append((t1 - t0, t2 - t1, p1.returncode, p2.returncode, len(p2.stdout)))
    print("run%02d connect=%.3fs first_call=%.3fs total=%.3fs rc=(%d,%d) out_bytes=%d"
          % (i, t1 - t0, t2 - t1, t2 - t0, p1.returncode, p2.returncode, len(p2.stdout)))

print("loadavg_end", open("/proc/loadavg").read().strip())
if rows:
    print("median connect=%.3fs first_call=%.3fs total=%.3fs"
          % (statistics.median([r[0] for r in rows]),
             statistics.median([r[1] for r in rows]),
             statistics.median([r[0] + r[1] for r in rows])))
    print("all_rc_zero", all(r[2] == 0 and r[3] == 0 for r in rows))
    print("stdout_nonempty_all", all(r[4] > 0 for r in rows))
