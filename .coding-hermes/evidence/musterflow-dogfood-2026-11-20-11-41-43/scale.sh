#!/usr/bin/env bash
# Scale arm: does per-invocation cost track SPEC BYTES (parse-bound) or not?
set -u
SB=/home/kara/.hermes/cache/scratch/r8
D=$SB/d3
mkdir -p "$D"
MF="$SB/mf"
python3 "$SB/gen_big.py" "$SB/clean.yaml" "$SB/big2.yaml" 1500
ls -l "$SB/big2.yaml" | awk '{print "bytes="$5}'
echo "== cold connect 10MB =="
/usr/bin/time -f "wall=%e user=%U sys=%S rc=%x" "$MF" --data-dir "$D" connect "$SB/big2.yaml" --name big2-inv 2>&1 | tail -2
echo "== warm op, 5 runs =="
hyperfine --warmup 1 --runs 5 --export-json "$SB/hf_big2.json" \
  -n big2 "$MF --data-dir $D big2-inv items list-items" > "$SB/hf_big2.txt" 2>&1
grep -E 'Time \(mean|Range' "$SB/hf_big2.txt"
jq -r '.results[]|"min=\(.min*1000|floor)ms median=\(.median*1000|floor)ms"' "$SB/hf_big2.json"
echo "== loadavg =="; cat /proc/loadavg
