#!/usr/bin/env bash
set -u
SB=/home/kara/.hermes/cache/scratch/r8
PORT=19877
B="http://127.0.0.1:$PORT"
H=(-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream')

echo "=== initialize ==="
curl -s -m 10 -D "$SB/mcp_head.txt" "${H[@]}" -X POST "$B/mcp" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"r8-dogfood","version":"0"}}}' | head -c 800
echo
SID=$(grep -i '^mcp-session-id:' "$SB/mcp_head.txt" | tr -d '\r' | awk '{print $2}')
echo "session=$SID"

echo "=== notifications/initialized ==="
curl -s -m 10 -o /dev/null -w 'rc=%{http_code}\n' "${H[@]}" -H "Mcp-Session-Id: $SID" -X POST "$B/mcp" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'

echo "=== tools/list ==="
curl -s -m 20 "${H[@]}" -H "Mcp-Session-Id: $SID" -X POST "$B/mcp" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' > "$SB/mcp_tools.txt"
wc -c < "$SB/mcp_tools.txt"
grep -o '"name":"[^"]*"' "$SB/mcp_tools.txt" | sort | head -30

echo "=== catalog search (DF-035) ==="
curl -s -m 10 -o "$SB/cat.json" -w 'catalog_rc=%{http_code}\n' "$B/api/catalog/search?q=github"
head -c 300 "$SB/cat.json"
echo
