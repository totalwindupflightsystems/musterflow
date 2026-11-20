#!/usr/bin/env bash
# CATALOG-002: assemble catalog/staging/pass2/seed-proposal.json from the
# per-candidate entry JSON the real generator produced (staged verbatim) plus
# the facts measured by the same run's connect/list transcripts.
#
# Usage: bash build-seed-proposal.sh <staging-dir> <transcript-dir>
set -euo pipefail
STAGE="${1:?staging dir}"
LOGS="${2:?transcript dir}"

# slug|seed category (pass 1)|pass-1 rank
META=(
  "vercel|infra / dev-tools|1"
  "huggingface|ai|2"
  "ollama|ai / local inference|3"
  "nws|data / public sector|4"
  "hetzner|infra|5"
)

tmp="$(mktemp -d)"
entries="$tmp/entries.jsonl"
: >"$entries"

for row in "${META[@]}"; do
  IFS='|' read -r slug category rank <<<"$row"
  log="$LOGS/gen-$slug.log"
  # Facts as measured by this run's `connect` + `list` output.
  base="$(awk -F': ' '/^    Base: /{print $2; exit}' "$log")"
  auth="$(awk -F': ' '/^    Auth: /{print $2; exit}' "$log")"
  endpoints="$(awk -F': ' '/^    Endpoints: /{print $2; exit}' "$log")"
  jq -c \
    --arg seed_id "$slug" \
    --arg category "$category" \
    --argjson rank "$rank" \
    --arg endpoints "$endpoints" \
    --arg base "$base" \
    --arg auth "$auth" \
    --arg staged "catalog/staging/pass2/$slug.json" \
    '. + {
      seed_id: $seed_id,
      seed_category: $category,
      pass1_rank: $rank,
      measured_endpoints: ($endpoints | tonumber),
      measured_base_url: $base,
      measured_auth_type: $auth,
      staged_file: $staged
    }' "$STAGE/$slug.json" >>"$entries"
done

jq -s \
  --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{
    proposal: "catalog/staging/pass2/seed-proposal.json",
    pass: 2,
    generated_at: $generated_at,
    generated_by: "musterflow catalog push (internal/catalog.ConnectionToCatalogEntry -> catalog.Score), one fresh-binary run per candidate on its own /tmp data dir and dashboard port 19881-19885",
    board_row: "CATALOG-002",
    related_board_rows: ["CATALOG-003"],
    kanban_card: "t_cafe0b6d",
    kanban_goal: "t_c54add61",
    goal_ac: "AC4",
    base_seed: {
      path: "catalog/seed.json",
      entries: 10,
      modified_by_this_pass: false
    },
    staging: {
      dir: "catalog/staging/pass2",
      entry_files: ["vercel.json", "huggingface.json", "ollama.json", "nws.json", "hetzner.json"],
      proposal: "seed-proposal.json"
    },
    score_basis: "catalog.Score(): +5 when the spec host matches the 20-domain official allowlist, +2 when type == \"openapi\", +1 when the description is longer than 20 chars, +1 when a version is set, +1 when downloads > 100; tier = official (>=7) / community-inferred (>=3) / untested (<3). Every score below is the value the shipped function computed for the generator output — none was hand-set or rounded up for this proposal.",
    score_caveat: "All five land at 1-2 / untested, and that is the honest output of the shipped formula, not a judgement of the APIs: (a) the official-domain bonus matches the SPEC host (e.g. api.vercel.com) while every one of these five serves its document from a docs/raw host (openapi.vercel.sh, huggingface.co/.well-known, raw.githubusercontent.com, docs.hetzner.cloud), so +5 is unreachable; (b) the +2 structured-spec bonus keys on type == \"openapi\" while ConnectionToCatalogEntry emits type \"api\", so it is unreachable for every generated entry; (c) nws declares a 15-char description, so it also misses the +1 description bonus. Raising these numbers needs an owner scoring-policy decision (match the API base host, settle the type vocabulary) — hand-inflating them here would defeat the point of staging them at all.",
    known_gap: "internal/catalog/scoring.go: the official-domain allowlist and the openapi-type bonus are both unreachable for generator-produced entries (proven by these five). Candidate follow-up row — not filed by this worker, board state belongs to the foreman.",
    measured_fields_note: "measured_endpoints / measured_base_url / measured_auth_type are read back from the connect + list output of this run for each scratch instance. All five connections record auth type \"none\": `connect` does not infer the security scheme from the document, even where the spec declares one (pass-1 evidence, docs/catalog/candidates-pass1.md, records the declared schemes per candidate). Recorded as measured rather than asserted.",
    promotion_notes: "Promoting an entry into catalog/seed.json maps id <- seed_id (seed ids are vendor slugs; the generator emits the connection id) and category <- seed_category (the generator emits no category). author/added_at/quality_tier/downloads are connection-scoped and stay as-is for the community catalog backend. Nothing in catalog/seed.json was modified by this pass.",
    owner_gated: [
      "publishing entries to github.com/totalwindupflightsystems/musterflow-catalog",
      "promoting any of these entries into catalog/seed.json"
    ],
    entries: .
  }' "$entries"
