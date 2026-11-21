# CATALOG-005 — staged entry scores, before and after

Task: make the official-domain (+5) and structured-spec (+2) bonuses in
`internal/catalog/scoring.go` reachable for entries produced by the generator
path (`ConnectionToCatalogEntry`).

Both numbers below come from the REAL generator — `catalog push` on a fresh,
scratch data dir after a live `connect` of the entry's spec URL — not from a
hand-written table:

- **before** = binary built from the pre-change tree (`git archive 4fef90f`),
  artifacts in `regen-prefix/`, transcripts in `logs-prefix/`
- **after** = binary built from this branch's tree
  (`go build -o /tmp/mf-cat005 ./cmd/musterflow/`), artifacts in `regen/`,
  transcripts in `logs/`

Harness: `regen-staging.sh` (this directory). Each candidate gets its own
`/tmp/mf-cat005-verify/<slug>` data dir and its own dashboard port (19891–19895),
so the canonical `~/.musterflow` store and port 9876 are never touched.

## Before → after

| id | spec_url | declared base | score before | score after | tier after |
|---|---|---|---|---|---|
| `c3ecb6f87d542f23` | `https://docs.hetzner.cloud/cloud.spec.json` | `https://api.hetzner.cloud/v1` | 2 | **9** | official |
| `8ac8f489bdf0ba6f` | `https://huggingface.co/.well-known/openapi.json` | `https://huggingface.co` | 2 | **9** | official |
| `70b5bf949a656585` | `https://api.weather.gov/openapi.json` | `https://api.weather.gov` | 1 | **8** | official |
| `3f26453ec4d52504` | `https://raw.githubusercontent.com/ollama/ollama/main/docs/openapi.yaml` | `http://localhost:11434` | 2 | **4** | community-inferred |
| `9a87419efed4870b` | `https://openapi.vercel.sh/` | `https://api.vercel.com` | 2 | **9** | official |

Every entry's id and spec_url are unchanged (same spec, same connection input);
only the score and tier move. All five are strictly above their pre-change
value, and four of five cross out of `untested` into `official`.

The before column also matches the `score` field recorded inside
`catalog/staging/pass2/*.json` (2/2/1/2/2) — i.e. the pre-change binary
reproduces the values CATALOG-002 committed.

## Why the before column was what it was

- **Official-domain bonus was unreachable.** `isOfficialDomain` substring-matched
  the whole spec URL against a list of API *base* hosts (`api.github.com`,
  `api.stripe.com`, …). A spec served from a provider's `docs.`/`openapi.` host
  (`docs.hetzner.cloud`, `openapi.vercel.sh`) or from a raw CDN
  (`raw.githubusercontent.com`) matched nothing, so the bonus never fired for
  these five. It now resolves the spec URL's **host** against the provider's
  registrable domain (so every docs/api/openapi host of a known provider counts),
  and additionally credits a docs-style spec host that sits on the same provider
  domain as the API base URL the entry declares (needs `BaseURL`, which
  `ConnectionToCatalogEntry` now carries).
- **Structured-spec bonus was unreachable.** It keyed on `entry.Type == "openapi"`,
  but the catalog's own vocabulary is `"api"`/`"workflow"`/`"wasm"` and
  `ConnectionToCatalogEntry` emits `"api"` — so no generated entry could ever
  earn it. It now credits an `"api"` entry that points at a spec document
  (`"openapi"` stays honored for older hand-written entries).

Per-entry arithmetic after the fix:

- hetzner: 5 (official: `hetzner.cloud` provider domain) + 2 (structured spec) + 1 (description) + 1 (version) = **9**
- huggingface: 5 (spec host == declared base host) + 2 + 1 + 1 = **9**
- nws: 5 (spec host == declared base host) + 2 + 0 (description "weather.gov API" is 15 chars, under the 20-char bar) + 1 = **8**
- ollama: 0 (its spec is GitHub's raw CDN, a third-party mirror, not ollama's own host) + 2 + 1 + 1 = **4**
- vercel: 5 (`vercel.sh` provider domain) + 2 + 1 + 1 = **9**

ollama is the one entry whose mirror does **not** earn the official bonus, on
purpose: `raw.githubusercontent.com` is not the provider's host, and crediting it
would also credit every random spec parked in any GitHub repository. The
structured-spec bonus still lifts it above its pre-change score.

## Cross-check from inside the package

`internal/catalog/staged_scores_test.go` (a `_test.go` helper, not a shipped
command) loads the same five staged entries and recomputes their scores with the
shipped `Score`. It agrees with the binary exactly:

```
c3ecb6f87d542f23  score_before=2 score_after=9  official           (hetzner)
8ac8f489bdf0ba6f  score_before=2 score_after=9  official           (huggingface)
70b5bf949a656585  score_before=1 score_after=8  official           (nws)
3f26453ec4d52504  score_before=2 score_after=4  community-inferred (ollama)
9a87419efed4870b  score_before=2 score_after=9  official           (vercel)
```

(The helper reads the staged files, which carry no `base_url` — the field did not
exist when CATALOG-002 staged them — so huggingface/nws there are credited by the
provider-domain arm rather than the equal-host arm. Same score either way.)

## Not rewritten: `catalog/staging/pass2/`

This row's brief limits writes to `internal/catalog` and
`.coding-hermes/evidence/CATALOG-005/`, so the pass-2 staged files were **not**
regenerated in place: their `score`/`quality_tier` fields still hold the
pre-change 2/2/1/2/2 and `untested`. The regenerated entries (with `base_url`,
new scores and `official` tiers) live in `regen/` and are what a promotion row
should stage from. Flagging this rather than silently leaving two disagreeing
copies of the same entry in the tree.

## Gates

| gate | result |
|---|---|
| `go test -short -count=1 ./internal/catalog/` | pass |
| `go test -short -count=1 ./internal/...` (whole module, short) | pass |
| `go vet ./internal/catalog/` | pass (no output) |
| `go build ./...` | pass |
| `go build -o /tmp/mf-cat005 ./cmd/musterflow/` | pass |
| `git diff catalog/seed.json` | empty (0 bytes) |

Full verbose tail of the extended score tests: `score-tests-tail.txt`.
