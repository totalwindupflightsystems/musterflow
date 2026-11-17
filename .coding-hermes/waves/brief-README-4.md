# Worker brief — README-4 (README accuracy bundle)

You are a coding worker in the musterflow repo. Your worktree is
`/home/kara/worktrees/musterflow-README-4`, branch `wt/README-4`. Work ONLY in that
worktree. Edit ONE file: `README.md`. Do NOT touch `docs/`, any `.go` file, the board
(`.coding-hermes/`), `go.mod`, or push anything.

Repo: Go 1.26 CLI, `github.com/totalwindupflightsystems/musterflow`. Build: `go build ./...`.

## Tasks (all in README.md)

1. **Testing section (README-4).** The README never mentions the test suite
   (`grep -in test README.md` → zero hits). Add a `## Testing` section immediately
   BEFORE `## License` (currently line 262) with the real commands:
   - `go test -short -count=1 ./...` — fast suite used by CI
   - `go test -count=1 ./...` — full suite
   State that CI (`.github/workflows/ci.yml`) runs build, vet and the suite.

2. **Configuration sample (README-5).** At `README.md:216` the first-run sample shows
   `data_dir: ~/.musterflow/`. The app writes the RESOLVED ABSOLUTE path on first run
   (e.g. `/home/<user>/.musterflow`, no trailing slash) — a reader scripting against the
   tilde literal gets different behavior. Fix the sample to show the resolved absolute
   path (`data_dir: /home/<user>/.musterflow`) and add one sentence: the tilde is
   expanded and the absolute path written on first run.

3. **Commands table (README-6).** `README.md:207` is `| \`musterflow import\` | Import API
   registry from JSONL |`. `musterflow import` with no argument fails
   `Error: accepts 1 arg(s), received 0`; the working form is
   `musterflow import ~/.musterflow/registry.jsonl`. Change the row to
   `` `musterflow import <file>` `` (match the style of the export row).

4. **Quality-scoring paragraph (DOC-7).** `README.md:166` claims weights
   "official domain (+5), endpoint count (+3), validated OpenAPI (+2), description
   presence (+1), examples (+1)". The real implementation is
   `internal/catalog/scoring.go:7-36`:
   - official domain → +5
   - `Type == "openapi"` (structured spec) → +2
   - description non-empty AND longer than 20 chars → +1
   - version non-empty → +1
   - downloads > 100 → +1
   - tiers: score >= 7 → `official`, >= 3 → `community-inferred`, else `untested`
   Rewrite the paragraph to match exactly (read the Go file yourself first, do not trust
   this summary blindly).

5. **Auth block formatting (DOC-23).** `README.md:107-108` jam two commands on one line and
   the line is malformed: `musterflow auth add gh --type apikey --key *** auth add db --type
   bearer --key eyJ...`. Split into separate, single-command lines. Before documenting any
   flag, verify it against the binary: `go build -o /tmp/mf-readme4 ./cmd/musterflow && /tmp/mf-readme4 auth add --help`.
   Do not invent flags; if `--type`/`--key` are wrong, document what the binary actually accepts.

## Forbidden

- Do NOT change the Go Version badge at `README.md:5` — it already reads `Go-1.26.11`,
  matching `go.mod`. It is out of scope.
- Do NOT rewrite unrelated README claims, do NOT edit other files, do NOT run
  `git push`, do NOT touch the board or `.gitreins/tasks.yaml`.

## Verify before commit

- `grep -n "## Testing" README.md`, `grep -n "musterflow import <file>" README.md`,
  `grep -n "data_dir:" README.md`.
- Code fences balanced: `grep -c '^\`\`\`' README.md` is even.
- `git diff --stat` shows only `README.md`.

## Commit

`git add README.md` and commit with a message like
`docs(README): testing section, config path, import arg, scoring weights`.
Never use `--no-verify` unless the guard fails for a reason you can quote; if it does,
disclose the bypass in the message body. Do NOT push. Report the commit SHA and the
exact lines you changed.

## Dispatch note (added by foreman)

This repo's gitreins pre-commit guard can SILENTLY NO-OP on non-Go diffs (exit 0, but
no commit is created — see board row DF-046). After `git commit`, ALWAYS verify with
`git log -1 --format=%H` that a NEW commit exists. If HEAD did not move, retry the
commit once; if it no-ops again, commit with `--no-verify` and disclose the bypass in
the commit message body (this is the sanctioned path for a silent guard no-op).
