#!/usr/bin/env bash
# Real-use probes: tolerant connect on a defective spec, refresh picking up spec changes, output-file formats.
set -u
SB=/home/kara/.hermes/cache/scratch/r8
MF="$SB/mf"
D=$SB/d5; rm -rf "$D"; mkdir -p "$D"
echo "== A. connect --tolerate on the defective vendor spec =="
"$MF" --data-dir "$D" connect "$SB/inventory.yaml" --tolerate --name vendor 2>&1 | tail -4
echo "tolerate_rc=$?"
echo "-- help surface --"
"$MF" --data-dir "$D" vendor --help 2>&1 | sed -n '1,25p'
echo "-- list-items --"
"$MF" --data-dir "$D" vendor list-items 2>&1 | head -8; echo "list_rc=$?"
echo "-- get-item A-100 --"
"$MF" --data-dir "$D" vendor get-item --sku A-100 2>&1 | head -8; echo "get_rc=$?"
echo "-- stock-levels --"
"$MF" --data-dir "$D" vendor stock-levels 2>&1 | head -8; echo "stock_rc=$?"

echo
echo "== B. refresh: does a spec change reach the CLI? =="
D6=$SB/d6; rm -rf "$D6"; mkdir -p "$D6"
cp "$SB/clean.yaml" "$SB/live.yaml"
"$MF" --data-dir "$D6" connect "$SB/live.yaml" --name live 2>&1 | tail -2
echo "before: warehouse op present? -> $("$MF" --data-dir "$D6" live --help 2>&1 | grep -c warehouse)"
python3 "$SB/addpath.py" "$SB/live.yaml"
"$MF" --data-dir "$D6" refresh live 2>&1 | tail -3; echo "refresh_rc=$?"
echo "after: warehouse op present? -> $("$MF" --data-dir "$D6" live --help 2>&1 | grep -c warehouse)"
echo "-- call the new op --"
"$MF" --data-dir "$D6" live warehouse 2>&1 | head -5; echo "newop_rc=$?"

echo
echo "== C. output-file formats =="
"$MF" --data-dir "$D6" live items list-items --output-file "$SB/out.csv" 2>&1 | tail -2; echo "csv_rc=$?"
head -3 "$SB/out.csv" 2>&1
"$MF" --data-dir "$D6" live items list-items --output-file "$SB/out.json" 2>&1 | tail -2; echo "json_rc=$?"
head -c 300 "$SB/out.json" 2>&1; echo
"$MF" --data-dir "$D6" live items list-items --output-file "$SB/out.txt" 2>&1 | tail -2; echo "txt_rc=$?"
head -4 "$SB/out.txt" 2>&1
echo "loadavg=$(cat /proc/loadavg)"
