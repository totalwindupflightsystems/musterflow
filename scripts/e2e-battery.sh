#!/usr/bin/env bash
# MusterFlow full E2E battery (in-repo copy of the tick-scoped harness, tick 235 2026-11-22).
# Origin: /tmp/mf-t1145-e2e-battery.sh (tick 235; structure from tick 233). Run from repo root:
#   bash scripts/e2e-battery.sh
# Fresh isolated data dir + scratch port 19878; kills its dashboard at the end.
# Expectation change vs tick233: hooks are POST-only since 3cf99b4 (GET -> 405),
# /api,/mcp,/hooks require bearer (dashboard_token in <data-dir>/config.yaml).
# musterflow tick musterflow-foreman-2026-11-22-11-45-40 FULL E2E battery (window opener; last full = tick 233 @ 8508c2c)
# Tree under test: master @ <merged HEAD incl. 3cf99b4 dashboard auth + d496d8d catalog fetch>
# Adapted from /tmp/mf-t233-e2e-battery.sh (tick233, 28 cells) + NEW auth-surface cells:
#   bearer token on /api,/mcp,/hooks (3cf99b4); hooks POST-only.
# Recipe: coding-hermes-foreman/references/musterflow-e2e-battery.md + tick-68 expectation fixes.
set -u
BIN=/tmp/mf-t1145-e2e-bin
DATADIR=/tmp/mf-t1145-e2e-data
PORT=19878
BASE="http://127.0.0.1:${PORT}"
TOK="mf-e2e-tick-20261122-token"
AUTH="Authorization: Bearer ${TOK}"
HOME_FLOWS="$HOME/.musterflow/flows"
HOME_DB="$HOME/.musterflow/musterflow.db"
PASS=0; FAIL=0; FAILED=""
ck() { if eval "$2"; then PASS=$((PASS+1)); else FAIL=$((FAIL+1)); FAILED="$FAILED [$1]"; echo "FAIL $1: $2"; fi; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

FLOWSRC='print(42)
print("trigger=" + ("none" if trigger == None else "set"))'

cd /home/kara/musterflow || exit 1
rm -rf "$DATADIR"; mkdir -p "$DATADIR"
printf 'dashboard_token: "%s"\n' "$TOK" > "$DATADIR/config.yaml"

go build -o "$BIN" ./cmd/musterflow
ck "01-build" '[ -x "$BIN" ]'

DB_MTIME_BEFORE=$(stat -c '%Y' "$HOME_DB" 2>/dev/null || echo missing)
ls "$HOME_FLOWS" 2>/dev/null | sort > /tmp/mf-t1145-homeflows-before.txt

"$BIN" start --data-dir "$DATADIR" --dashboard-addr "127.0.0.1:$PORT" >/tmp/mf-t1145-e2e-dash.log 2>&1 &
DASHPID=$!
HEALTH=000
for i in $(seq 1 30); do sleep 1; HEALTH=$(code "$BASE/api/health"); [ "$HEALTH" = "200" ] && break; done
ck "02-health-up" '[ "$HEALTH" = "200" ]'

"$BIN" flow create e2e-verify --source "$FLOWSRC" --description "e2e desc" --data-dir "$DATADIR" >/tmp/mf-t1145-c1.log 2>&1
ck "03-flow-create" '[ $? -eq 0 ]'
"$BIN" flow list --data-dir "$DATADIR" 2>/dev/null | grep -q e2e-verify
ck "04-flow-listed" '[ $? -eq 0 ]'

"$BIN" flow run e2e-verify --data-dir "$DATADIR" >/tmp/mf-t1145-run.out 2>/tmp/mf-t1145-run.err
EXPECT_STDOUT=$(printf '42\ntrigger=none')
ck "05-run-stdout-exact" '[ "$(cat /tmp/mf-t1145-run.out)" = "$EXPECT_STDOUT" ]'
ck "06-run-stderr-empty" '[ ! -s /tmp/mf-t1145-run.err ]'

ck "07-source-roundtrip" '[ "$(cat "$DATADIR/flows/e2e-verify.star")" = "$(printf "%s" "$FLOWSRC")" ]'
ck "08-sidecar-desc" "$(printf 'jq -r .description "$DATADIR/flows/e2e-verify.star.json" 2>/dev/null | grep -q "e2e desc"')"

"$BIN" flow create hook-flow --source "$FLOWSRC" --webhook --data-dir "$DATADIR" >/dev/null 2>&1
ck "09-hook-create" '[ $? -eq 0 ]'

FLOWS_ARR=$(curl -s -H "$AUTH" "$BASE/api/flows" | jq -c ".flows")
ck "10-e2e-verify-no-webhook" '[ "$(printf "%s" "$FLOWS_ARR" | jq -r ".[] | select(.name==\"e2e-verify\") | [.webhook, (.webhook_url == null)] | @tsv")" = "false	true" ]'
ck "11-hook-has-url" '[ "$(printf "%s" "$FLOWS_ARR" | jq -r ".[] | select(.name==\"hook-flow\") | [.webhook, (.webhook_url != null and (.webhook_url | contains(\":'"$PORT"'/hooks/hook-flow\")))] | @tsv")" = "true	true" ]'

ck "12-data-flows-dir" '[ -f "$DATADIR/flows/e2e-verify.star" ]'
ls "$HOME_FLOWS" 2>/dev/null | sort > /tmp/mf-t1145-homeflows-after.txt
ck "13-home-flows-untouched" 'diff -q /tmp/mf-t1145-homeflows-before.txt /tmp/mf-t1145-homeflows-after.txt >/dev/null'
DB_MTIME_AFTER=$(stat -c '%Y' "$HOME_DB" 2>/dev/null || echo missing)
ck "14-home-db-mtime" '[ "$DB_MTIME_BEFORE" = "$DB_MTIME_AFTER" ]'

"$BIN" flow create compute-flow --source "print(6 * 7)" --data-dir "$DATADIR" >/dev/null 2>&1
R=$( "$BIN" flow run compute-flow --data-dir "$DATADIR" 2>/dev/null )
ck "15-starlark-real-exec" '[ "$R" = "42" ]'

# --- API surface: auth enforced (except /api/health) ---
ck "16-api-health-notoken" '[ "$(code "$BASE/api/health")" = "200" ]'
ck "17-api-apis-notoken-401" '[ "$(code "$BASE/api/apis")" = "401" ]'
ck "18-api-apis-token" '[ "$(code -H "$AUTH" "$BASE/api/apis")" = "200" ]'
ck "19-api-apis-unknown-404" '[ "$(code -H "$AUTH" "$BASE/api/apis/unknown-xyz")" = "404" ]'
ck "20-api-flows-token" '[ "$(code -H "$AUTH" "$BASE/api/flows")" = "200" ]'
EXPECT_API_RUN='{"result":"42\ntrigger=none"}'
APIRUN=$(curl -s -X POST -H "$AUTH" "$BASE/api/flows/e2e-verify/run")
ck "21-api-run-envelope" '[ "$APIRUN" = "$EXPECT_API_RUN" ]'
ck "22-api-run-404" '[ "$(code -X POST -H "$AUTH" "$BASE/api/flows/nope/run")" = "404" ]'
ck "23-api-catalog-search" '[ "$(code -H "$AUTH" "$BASE/api/catalog/search")" = "200" ]'
ck "24-mcp-info-notoken-401" '[ "$(code "$BASE/api/mcp/info")" = "401" ]'
ck "25-mcp-info-token" '[ "$(code -H "$AUTH" "$BASE/api/mcp/info")" = "200" ]'
MCPOUT=$(curl -s -X POST -H "$AUTH" -H "Content-Type: application/json" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}" "$BASE/mcp")
ck "26-mcp-tools-envelope" '[ "$MCPOUT" = "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}" ]'
MCPEMPTY=$(curl -s -X POST -H "$AUTH" "$BASE/mcp")
ck "27-mcp-empty-parse-err" '[ "$(printf "%s" "$MCPEMPTY" | jq -r ".error.code")" = "-32700" ]'
ck "28-mcp-notoken-401" '[ "$(code -X POST "$BASE/mcp")" = "401" ]'

# --- hooks: bearer required, POST-only ---
ck "29-hook-notoken-401" '[ "$(code -X POST "$BASE/hooks/hook-flow")" = "401" ]'
ck "30-hook-get-405" '[ "$(code -H "$AUTH" "$BASE/hooks/hook-flow")" = "405" ]'
HOOKOUT=$(curl -s -X POST -H "$AUTH" "$BASE/hooks/hook-flow")
ck "31-hook-post-result" '[ "$HOOKOUT" = "$EXPECT_API_RUN" ]'

ck "32-root-public" '[ "$(code "$BASE/")" = "200" ]'

kill $DASHPID 2>/dev/null
sleep 2
PORT_DEAD=$(code "$BASE/api/health")
ck "33-port-released" '[ "${PORT_DEAD:0:3}" = "000" ]'

echo "=================================="
echo "BATTERY RESULT: $PASS PASS / $FAIL FAIL of $((PASS+FAIL))"
[ -n "$FAILED" ] && echo "FAILED CELLS:$FAILED" || echo "ALL CELLS GREEN"
echo "BATTERY-N: $((PASS+FAIL))"
