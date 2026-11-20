#!/usr/bin/env bash
set -u
SB=/home/kara/.hermes/cache/scratch/r8
echo "=== config show ==="
$SB/mf --data-dir "$SB/data" config show 2>&1 | head -20
echo "=== does 'config set' exist? ==="
$SB/mf --data-dir "$SB/data" config set --help 2>&1 | head -15
echo "=== route via --dashboard-addr (no --data-dir) ==="
$SB/mf --dashboard-addr 127.0.0.1:19877 clean-inventory items list-items 2>&1 | head -12
echo "=== both flags (DF-033 suppression) ==="
$SB/mf --data-dir "$SB/data" --dashboard-addr 127.0.0.1:19877 clean-inventory items list-items 2>&1 | head -12
