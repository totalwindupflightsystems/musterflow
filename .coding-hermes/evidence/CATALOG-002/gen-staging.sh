#!/usr/bin/env bash
# CATALOG-002 pass-2 harness: for each pass-1 verified candidate, build nothing
# (the binary is passed in), connect on its own scratch data dir with the
# dashboard pinned off the canonical 9876 port, and capture the community
# catalog entry JSON the real generator (`catalog push`) prints.
#
# Usage: bash gen-staging.sh <binary> <out-staging-dir> <transcript-dir>
set -uo pipefail
BIN="${1:?binary}"
STAGE="${2:?staging dir}"
LOGS="${3:?transcript dir}"
mkdir -p "$STAGE" "$LOGS"

# slug|dashboard port (19881+, never 9876)|spec URL
CANDIDATES=(
  "vercel|19881|https://openapi.vercel.sh/"
  "huggingface|19882|https://huggingface.co/.well-known/openapi.json"
  "ollama|19883|https://raw.githubusercontent.com/ollama/ollama/main/docs/openapi.yaml"
  "nws|19884|https://api.weather.gov/openapi.json"
  "hetzner|19885|https://docs.hetzner.cloud/cloud.spec.json"
)

rc_all=0
for row in "${CANDIDATES[@]}"; do
  IFS='|' read -r slug port spec <<<"$row"
  scratch="/tmp/mf-cat002-verify/$slug"
  log="$LOGS/gen-$slug.log"
  rm -rf "$scratch"; mkdir -p "$scratch"
  : >"$log"

  echo "=== CATALOG-002 pass-2 — candidate $slug ===" | tee -a "$log"
  echo "binary:      $BIN" | tee -a "$log"
  echo "data dir:    $scratch (scratch; canonical ~/.musterflow untouched)" | tee -a "$log"
  echo "dashboard:   127.0.0.1:$port (canonical 9876 deliberately NOT used)" | tee -a "$log"
  echo "spec:        $spec" | tee -a "$log"
  echo | tee -a "$log"

  "$BIN" --data-dir "$scratch" --dashboard-addr ":$port" start >>"$log" 2>&1 &
  dash_pid=$!
  ready=0
  for _ in $(seq 1 120); do
    if curl -fsS "http://127.0.0.1:$port/api/health" 2>/dev/null | grep -q '"status":"ok"'; then
      ready=1; break
    fi
    if ! kill -0 "$dash_pid" 2>/dev/null; then break; fi
    sleep 0.5
  done
  echo "\$ curl http://127.0.0.1:$port/api/health -> ready=$ready" | tee -a "$log"
  if [[ "$ready" != "1" ]]; then
    echo "FATAL: dashboard on :$port never became healthy for $slug" | tee -a "$log"
    kill "$dash_pid" 2>/dev/null; wait "$dash_pid" 2>/dev/null
    rc_all=1; continue
  fi

  echo | tee -a "$log"
  echo "\$ $BIN --data-dir $scratch --dashboard-addr 127.0.0.1:$port connect $spec --name $slug" | tee -a "$log"
  "$BIN" --data-dir "$scratch" --dashboard-addr "127.0.0.1:$port" connect "$spec" --name "$slug" >>"$log" 2>&1
  rc=$?
  echo "exit=$rc" | tee -a "$log"
  if [[ $rc -ne 0 ]]; then
    echo "FATAL: connect failed for $slug" | tee -a "$log"
    kill "$dash_pid" 2>/dev/null; wait "$dash_pid" 2>/dev/null
    rc_all=1; continue
  fi

  api_id="$(awk '/^  ID: /{print $2; exit}' "$log")"
  echo | tee -a "$log"
  echo "\$ $BIN --data-dir $scratch --dashboard-addr 127.0.0.1:$port list" | tee -a "$log"
  "$BIN" --data-dir "$scratch" --dashboard-addr "127.0.0.1:$port" list >>"$log" 2>&1
  echo "exit=$?" | tee -a "$log"

  echo | tee -a "$log"
  echo "\$ $BIN --data-dir $scratch --dashboard-addr 127.0.0.1:$port catalog push $api_id" | tee -a "$log"
  "$BIN" --data-dir "$scratch" --dashboard-addr "127.0.0.1:$port" catalog push "$api_id" >>"$log" 2>&1
  rc=$?
  echo "exit=$rc" | tee -a "$log"
  if [[ $rc -ne 0 ]]; then
    echo "FATAL: catalog push failed for $slug" | tee -a "$log"
    kill "$dash_pid" 2>/dev/null; wait "$dash_pid" 2>/dev/null
    rc_all=1; continue
  fi

  # The generator's JSON block is the MarshalIndent body between the framing
  # lines; capture it byte-for-byte rather than re-serializing by hand.
  awk '/^\{$/{f=1} f{print} f && /^\}$/{exit}' "$log" >"$STAGE/$slug.json"
  echo | tee -a "$log"
  echo "staged: $STAGE/$slug.json ($(wc -c <"$STAGE/$slug.json") bytes)" | tee -a "$log"
  jq -e '.id and .spec_url and (.score|type=="number")' "$STAGE/$slug.json" >/dev/null \
    && echo "validated: parses as JSON with id, spec_url, score" | tee -a "$log" \
    || { echo "FATAL: staged entry for $slug fails the id/spec_url/score check" | tee -a "$log"; rc_all=1; }

  kill "$dash_pid" 2>/dev/null; wait "$dash_pid" 2>/dev/null
  echo "dashboard on :$port stopped" | tee -a "$log"
  echo | tee -a "$log"
done

exit "$rc_all"
