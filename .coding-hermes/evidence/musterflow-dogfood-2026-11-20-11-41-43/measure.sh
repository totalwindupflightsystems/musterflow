#!/usr/bin/env bash
# Perf arm: per-invocation cost of one identical CLI operation against a small
# (2.5 KB) vs large (4.3 MB) spec in the SAME store, same window. Both ops hit
# the local mock API on 127.0.0.1:18099, so the only variable is spec size.
set -u
SB=/home/kara/.hermes/cache/scratch/r8
D=$SB/d2
mkdir -p "$D"
MF="$SB/mf"

echo "== loadavg before =="; cat /proc/loadavg; echo "cores=$(nproc)"

echo "== connect help =="; "$MF" connect --help 2>&1 | head -18

echo "== cold connect: small (2.5K) =="
/usr/bin/time -f "wall=%e user=%U sys=%S rc=%x" "$MF" --data-dir "$D" connect "$SB/clean.yaml" --name small-inv 2>&1 | tail -4

echo "== cold connect: big (4.3M) =="
/usr/bin/time -f "wall=%e user=%U sys=%S rc=%x" "$MF" --data-dir "$D" connect "$SB/big.yaml" --name big-inv 2>&1 | tail -4

echo "== store =="; ls -lh "$D" | head

echo "== single run big =="
"$MF" --data-dir "$D" big-inv items list-items > "$SB/call_big.out" 2>"$SB/call_big.err"; echo "rc=$?"
head -c 300 "$SB/call_big.out"; echo; head -3 "$SB/call_big.err"

echo "== single run small =="
"$MF" --data-dir "$D" small-inv items list-items > "$SB/call_small.out" 2>"$SB/call_small.err"; echo "rc=$?"
head -c 300 "$SB/call_small.out"; echo; head -3 "$SB/call_small.err"

echo "== hyperfine A: big then small =="
hyperfine --warmup 2 --runs 10 --export-json "$SB/hf_ab.json" \
  -n big "$MF --data-dir $D big-inv items list-items" \
  -n small "$MF --data-dir $D small-inv items list-items" > "$SB/hf_ab.txt" 2>&1
echo "hfA_rc=$?"

echo "== hyperfine B: small then big (order swapped) =="
hyperfine --warmup 2 --runs 10 --export-json "$SB/hf_ba.json" \
  -n small "$MF --data-dir $D small-inv items list-items" \
  -n big "$MF --data-dir $D big-inv items list-items" > "$SB/hf_ba.txt" 2>&1
echo "hfB_rc=$?"

echo "== loadavg after =="; cat /proc/loadavg
