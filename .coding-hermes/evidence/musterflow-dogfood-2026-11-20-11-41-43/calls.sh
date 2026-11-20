#!/usr/bin/env bash
set -u
SB=/home/kara/.hermes/cache/scratch/r8
B="http://127.0.0.1:19877"
H=(-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream')

echo "=== MCP tools/call clean-inventory__listItems ==="
curl -s -m 20 "${H[@]}" -X POST "$B/mcp" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"clean-inventory__listItems","arguments":{}}}' | head -c 900
echo
echo "=== MCP tools/call listItems (bare alias) ==="
curl -s -m 20 "${H[@]}" -X POST "$B/mcp" \
  -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"listItems","arguments":{}}}' | head -c 400
echo
echo "=== MCP call of tolerant API tool (should not exist) ==="
curl -s -m 20 "${H[@]}" -X POST "$B/mcp" \
  -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"warehouse-inventory-api__listItems","arguments":{}}}' | head -c 500
echo
echo "=== CLI call: table ==="
$SB/mf --data-dir "$SB/data" clean-inventory items list-items 2>&1 | head -10
echo "=== CLI call: json ==="
$SB/mf --data-dir "$SB/data" clean-inventory items list-items --output-file "$SB/out.json" 2>&1 | head -5
head -c 300 "$SB/out.json" 2>/dev/null
echo
echo "=== CLI POST createItem ==="
$SB/mf --data-dir "$SB/data" clean-inventory items create-item --body '{"sku":"D-400","qty":5}' 2>&1 | head -8
