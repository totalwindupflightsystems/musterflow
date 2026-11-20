#!/usr/bin/env bash
# Round-8 MCP/dashboard leg: start on a private port with an isolated data dir.
set -u
SB=/home/kara/.hermes/cache/scratch/r8
PORT=19877
pkill -f "r8/mf --data-dir $SB/data start" 2>/dev/null
sleep 0.3
nohup $SB/mf --data-dir "$SB/data" --dashboard-addr "127.0.0.1:$PORT" start >"$SB/dash.log" 2>&1 &
echo "pid=$!"
for i in $(seq 1 40); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/api/apis" 2>/dev/null)
  if [ "$code" = "200" ]; then echo "up_after=${i}00ms"; break; fi
  sleep 0.1
done
echo "--- /api/apis ---"
curl -s "http://127.0.0.1:$PORT/api/apis" | head -c 1200
echo
echo "--- dash.log ---"
head -20 "$SB/dash.log"
