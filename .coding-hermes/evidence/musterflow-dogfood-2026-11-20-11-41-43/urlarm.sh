#!/usr/bin/env bash
# Arm 1: connect by URL -> is the spec re-downloaded on EVERY operation call?
# Arm 2: MCP tools/list latency with a large API connected (does it re-parse per request?)
set -u
SB=/home/kara/.hermes/cache/scratch/r8
MF="$SB/mf"
D=$SB/d4; mkdir -p "$D"
SERVE=$SB/serve; mkdir -p "$SERVE"; cp "$SB/big.yaml" "$SERVE/big.yaml"

echo "== start static spec server :19901 =="
pkill -f "http.server 19901" 2>/dev/null
( cd "$SERVE" && nohup python3 -m http.server 19901 --bind 127.0.0.1 > "$SB/spec_srv.log" 2>&1 & echo $! > "$SB/spec_srv.pid" )
sleep 1
curl -s -o /dev/null -w "spec_server_code=%{http_code} bytes=%{size_download}\n" http://127.0.0.1:19901/big.yaml

echo "== connect BY URL =="
/usr/bin/time -f "connect_url wall=%e user=%U sys=%S rc=%x" "$MF" --data-dir "$D" connect http://127.0.0.1:19901/big.yaml --name bigurl 2>&1 | tail -3
echo "Gets after connect: $(grep -c 'GET /big.yaml' "$SB/spec_srv.log")"

echo "== 3 operation calls (each one a fresh process) =="
for i in 1 2 3; do
  /usr/bin/time -f "call$i wall=%e user=%U sys=%S rc=%x" "$MF" --data-dir "$D" bigurl items list-items > "$SB/urlcall_$i.out" 2>>"$SB/urlcall_t.err"
  head -1 "$SB/urlcall_$i.out"
done
tail -3 "$SB/urlcall_t.err"
echo "Gets after 3 calls: $(grep -c 'GET /big.yaml' "$SB/spec_srv.log")"

echo "== dashboard on :19902 with this store =="
pkill -f "r8/mf --data-dir $D" 2>/dev/null; sleep 1
nohup "$MF" --data-dir "$D" --dashboard-addr 127.0.0.1:19902 start > "$SB/dash4.log" 2>&1 &
echo $! > "$SB/dash4.pid"
for i in $(seq 1 40); do curl -s -o /dev/null http://127.0.0.1:19902/api/apis && break; sleep 0.1; done
curl -s http://127.0.0.1:19902/api/apis | head -c 300; echo

echo "== MCP tools/list x3 =="
H=$(mktemp); SI=""
curl -s -D "$H" -o "$SB/mcp_init4.json" -X POST http://127.0.0.1:19902/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"r8","version":"1"}}}'
SI=$(grep -i '^mcp-session-id:' "$H" | tr -d '\r' | awk '{print $2}')
echo "session=${SI:0:12}..."
curl -s -o /dev/null -X POST http://127.0.0.1:19902/mcp -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' -H "mcp-session-id: $SI" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
for i in 1 2 3; do
  curl -s -o "$SB/tl_$i.json" -w "tools_list_$i time=%{time_total}s bytes=%{size_download}\n" \
    -X POST http://127.0.0.1:19902/mcp -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' -H "mcp-session-id: $SI" \
    -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
done
echo "tool_names_count=$(grep -o '"name":"[^"]*"' "$SB/tl_3.json" | wc -l)"
echo "loadavg=$(cat /proc/loadavg)"
