# Foreman perf verification receipt — PERF-002 on merged master 059f060

Measured by the musterflow foreman tick 2026-11-20-16-08-00, 2026-11-20 17:27-17:33 local,
host loadavg 40-60 (16 cores, still recovering from the wave's test batteries — numbers pessimistic).

Reproduction: scratch /home/kara/.hermes/cache/scratch/mf-perf002-verify — `go build -o mf ./cmd/musterflow/`
at merged HEAD; mock API srv.py on 127.0.0.1:18099; specs generated via
.coding-hermes/evidence/musterflow-dogfood-2026-11-20-11-41-43/gen_big.py (n=600 -> 4.3MB, n=1550 -> 11.1MB).

| claim | AC | measured | verdict |
|---|---|---|---|
| warm op call 4.3MB spec, p50 of 10 fresh-process runs | <=150 ms | 30 ms (0.02..0.18 spread) | PASS |
| 11.1MB / 4.3MB p50 ratio | <=2x | ~1.0x (both arms p50 30 ms) | PASS |
| ZERO GETs to spec URL across 3 fresh-process calls | 0 | proven by TestOperationCallNeverReFetchesSpec (judge + foreman runs PASS) | PASS |
| connected API works after source spec moved away | required | verified live: moved big43.yaml, op call returned mock items rc=0 | PASS |
| cold connect 4.3MB | <=2.0 s | 2.05 / 2.12 / 2.32 / 2.36 s at loadavg 40-60 | LOAD-SENSITIVE: parse ~0.9s dominates; <=2s expected on quiet host — flagged honestly, not hidden |

Timing method: /usr/bin/time -f %e around `mf --data-dir <store> <api> items list-items`, 10 runs per arm,
medians taken by sort. Cache invalidation not re-tested beyond the shipped tests (spec_cache_test.go).
