# CATALOG-002 — catalog pass 2: run summary

Worktree: `/home/kara/worktrees/musterflow-CATALOG-002` (branch `wt/CATALOG-002`)
Binary:   `/tmp/mf-cat002` from `go build -o /tmp/mf-cat002 ./cmd/musterflow/`, exit 0
sha256:   `2d2391ec3f6b478e7ec68708ccbc3131bfe99cc2dd8c80a06847422ef824a597`
Feature commit: `26721b1` (15 files, staged entries + generator fix + these transcripts)

## AC4 citation

Drives kanban goal **t_c54add61 AC4** (via child card **t_cafe0b6d**) and closes out board rows
**CATALOG-002** (this pass) and **CATALOG-003** (its sibling: "Catalog pass 2: tolerance path
unblocks the cosmetic-spec rejections", board status `complete`) — the tolerance opt-in is not
exercised here because all five pass-1 subjects parse without it, so the two rows are independent
halves of pass 2 rather than one blocking the other.

## AC1 — five staged entry JSON files, one per verified candidate

Each candidate was connected on its **own scratch data dir** (`/tmp/mf-cat002-verify/<slug>`)
with a dashboard pinned to its **own port** (19881-19885). The canonical dashboard port **9876**
and the canonical `~/.musterflow` data dir were never used (`ss -tln` checked before the run).

| # | staged file | id | spec_url | score | tier | endpoints | base URL | dashboard port |
|---|---|---|---|---|---|---|---|---|
| 1 | `catalog/staging/pass2/vercel.json` | `9a87419efed4870b` | https://openapi.vercel.sh/ | 2 | untested | 493 | https://api.vercel.com | 19881 |
| 2 | `catalog/staging/pass2/huggingface.json` | `8ac8f489bdf0ba6f` | https://huggingface.co/.well-known/openapi.json | 2 | untested | 386 | https://huggingface.co | 19882 |
| 3 | `catalog/staging/pass2/ollama.json` | `3f26453ec4d52504` | https://raw.githubusercontent.com/ollama/ollama/main/docs/openapi.yaml | 2 | untested | 17 | http://localhost:11434 | 19883 |
| 4 | `catalog/staging/pass2/nws.json` | `70b5bf949a656585` | https://api.weather.gov/openapi.json | 1 | untested | 69 | https://api.weather.gov | 19884 |
| 5 | `catalog/staging/pass2/hetzner.json` | `c3ecb6f87d542f23` | https://docs.hetzner.cloud/cloud.spec.json | 2 | untested | 190 | https://api.hetzner.cloud/v1 | 19885 |

Every file parses as JSON and carries `id`, `spec_url` and `score` (checked per candidate in the
harness: `jq -e '.id and .spec_url and (.score|type=="number")'`). The files are the generator's
output verbatim — the harness slices the `Catalog entry JSON for …:` block out of the push
transcript rather than re-serializing anything by hand:

    awk '/^\{$/{f=1} f{print} f && /^\}$/{exit}' gen-<slug>.log

## AC2 — seed proposal, and `catalog/seed.json` untouched

`catalog/staging/pass2/seed-proposal.json` carries the five entries (generator fields verbatim)
plus provenance and measured facts: `seed_id`, `seed_category`, `pass1_rank`,
`measured_endpoints`, `measured_base_url`, `measured_auth_type`, `staged_file`. Top-level
fields record the board row, card, goal/AC, the score formula (`score_basis`), why the numbers
are what they are (`score_caveat`), the promotion mapping, and what stays owner-gated.

`catalog/seed.json` is unchanged: `git show --stat HEAD` lists no seed file, `git diff HEAD~1 HEAD
-- catalog/seed.json` is empty, and `git status --short` after the pass names nothing. Promoting
auth-walled defaults (Vercel, Hetzner) and publishing to `musterflow-catalog` are owner decisions
and were deliberately not done.

### Scores are the formula's honest output, not judgement calls

All five land at 1-2 / `untested` because of two structural gaps in `internal/catalog/scoring.go`,
both recorded in `seed-proposal.json` as `known_gap` + `score_caveat`:

1. The +5 official-domain bonus matches the **spec** host (`api.vercel.com`) but these five all
   serve their document from a docs/raw host (`openapi.vercel.sh`, `huggingface.co/.well-known`,
   `raw.githubusercontent.com`, `docs.hetzner.cloud`) — the bonus is unreachable.
2. The +2 structured-spec bonus keys on `type == "openapi"`, while `ConnectionToCatalogEntry`
   emits `type: "api"` — unreachable for **every** generator-produced entry.

nws additionally misses the +1 description bonus (its declared description is 15 chars). Raising
these values is a scoring-policy decision for the owner (match the API base host; settle the type
vocabulary); nothing was hand-inflated.

## AC3 — generator exercised once per candidate, full transcript saved

Transcripts (connect + list + push, with the dashboard boot and the routing note):

    .coding-hermes/evidence/CATALOG-002/gen-vercel.log
    .coding-hermes/evidence/CATALOG-002/gen-huggingface.log
    .coding-hermes/evidence/CATALOG-002/gen-ollama.log
    .coding-hermes/evidence/CATALOG-002/gen-nws.log
    .coding-hermes/evidence/CATALOG-002/gen-hetzner.log

Reproducible: `gen-staging.sh` (the harness) and `build-seed-proposal.sh` (the proposal builder)
are committed next to the logs.

    bash .coding-hermes/evidence/CATALOG-002/gen-staging.sh /tmp/mf-cat002 catalog/staging/pass2 .coding-hermes/evidence/CATALOG-002
    bash .coding-hermes/evidence/CATALOG-002/build-seed-proposal.sh catalog/staging/pass2 .coding-hermes/evidence/CATALOG-002 > catalog/staging/pass2/seed-proposal.json

## Generator fix (`internal/catalog/push.go`)

The pass-1 generator emitted `"score": 0` and `"quality_tier": "community-inferred"` as
hardcoded literals while `internal/catalog/scoring.go` ships a tested `Score()` that nothing
called — a placeholder zero that also contradicted its own tier. The entry now takes both from
`Score(entry)`.

RED proof (one file reverted, new tests kept staged):

    --- FAIL: TestConnectionToCatalogEntry
        push_test.go:59: Score: got 0, want 2
        push_test.go:62: QualityTier: got "community-inferred", want "untested"
    --- FAIL: TestConnectionToCatalogEntryScoreIsComputed/official_host_plus_metadata
        push_test.go:112: Score: got 0, want 7
        push_test.go:123: tier/score disagree: score 0 maps to "untested", entry says "community-inferred"

Restored file → `ok github.com/totalwindupflightsystems/musterflow/internal/catalog`.

## Gates

- `go build ./...` — ok
- `go test -short -count=1 ./...` — ok, 12 packages
- gitreins pre-commit guard (`.gitreins/logs/guard-20261120T164124.415179Z.log`): `go_build ok`,
  `go_lint clean`, `go_tests` ran the full suite and passed — no short-circuit, no `--no-verify`

## Left open (deliberately)

- Publishing the five entries to `totalwindupflightsystems/musterflow-catalog` (owner-gated).
- Promoting any of them into `catalog/seed.json` (owner-gated, and needs the scoring-policy call).
- `internal/catalog/scoring.go` reachability gap above — a candidate follow-up row the foreman
  may file; this worker does not write board state.
