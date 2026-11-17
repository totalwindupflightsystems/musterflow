# Worker brief — DOC-8 (docs accuracy bundle)

You are a coding worker in the musterflow repo. Your worktree is
`/home/kara/worktrees/musterflow-DOC-8`, branch `wt/DOC-8`. Work ONLY in that worktree.
Edit ONE file: `docs/integration-guide.md`. Do NOT touch `README.md` (another worker owns
it this tick), any `.go` file except for READ-ONLY inspection, the board
(`.coding-hermes/`), or `go.mod`. Do NOT push.

Repo: Go 1.26 CLI, `github.com/totalwindupflightsystems/musterflow`.

## Tasks (all in docs/integration-guide.md)

1. **Data-directory layout invents a file that does not exist (DOC-8).** Around lines
   640-656 the guide shows a `~/.musterflow/` tree containing
   `├── credentials.yaml       # Masked API credentials`. There is no `credentials.yaml`:
   credentials live in `config.yaml` under a top-level `auth:` map
   (`internal/config/config.go:20` — `Auth map[string]AuthConfig`; the config file is
   written mode 0600 because it carries secrets, see the comment at `config.go:93`).
   Remove the phantom line and state that per-API credentials are stored in `config.yaml`
   under the `auth:` key (masked by `musterflow auth list`). Keep the rest of the tree
   accurate — verify entries against a real data dir (`ls ~/.musterflow/`: `config.yaml`,
   `musterflow.db`, `flows/`, `transforms/`).

2. **Dashboard endpoints block misses two real routes (DOC-1).** The `### Endpoints` code
   block (lines ~113-131) lists neither `GET /api/export` nor `POST /api/import`, although
   both are registered at `internal/dashboard/server.go:68-69`. Add:
   - `GET  /api/export   → 200  registry as JSONL (application/x-ndjson)`
   - `POST /api/import   → 200  import connections from a JSONL body → {"imported":N}`
   and one short paragraph: export and import are a matched pair — export emits one JSON
   object per line and import consumes exactly that stream, replacing same-ID entries, so
   connections move between hosts (backup/transfer) without touching DuckDB directly.
   Cross-check wording against `specs/dashboard.md` §2.3 (rows 8 and 9) rather than inventing.

3. **DuckDB single-writer lock pitfall (DF-MUSTERFLOW-5-R4).** Document, in the
   `## Ports & Configuration` section (or an adjacent "Operational pitfalls" subsection):
   a running `musterflow start` holds a single-writer lock on `<data-dir>/musterflow.db`.
   A second process pointed at the same data dir fails with
   `Conflicting lock is held in .../musterflow.db` (see `internal/app/lockerr.go`). The
   remedy is to let the CLI route through the running dashboard (the default), or to use a
   separate `--data-dir` for local/offline work. Note that `--no-dashboard` against the
   live data dir is exactly the failing case.

4. **`--body` semantics (DF-MUSTERFLOW-5-R5).** Generated leaves accept a `--body` flag. Its
   semantics are undocumented: the value is JSON-encoded, it REPLACES the whole request
   body, and per-property flags are ignored when `--body` is supplied; a non-JSON value
   errors `invalid JSON for --body`. Add a short subsection under `## CLI Reference`
   documenting both behaviours. Verify the error text against the built binary
   (`go build -o /tmp/mf-doc8 ./cmd/musterflow && /tmp/mf-doc8 --help`) before quoting it;
   quote what you actually observe.

## Forbidden

- Do NOT edit `README.md` or any Go source. Do NOT push, do NOT touch the board,
  `.gitreins/tasks.yaml`, or `go.mod`.

## Verify before commit

- `grep -n "credentials.yaml" docs/integration-guide.md` → no hits.
- `grep -n "api/export" docs/integration-guide.md` and `grep -n "api/import"` → both present.
- `grep -n "Conflicting lock" docs/integration-guide.md` → present.
- `grep -n -- "--body" docs/integration-guide.md` → present.
- Code fences balanced: `grep -c '^\`\`\`' docs/integration-guide.md` is even.
- `git diff --stat` shows only `docs/integration-guide.md`.

## Commit

`git add docs/integration-guide.md` and commit with a message like
`docs(integration-guide): data dir, export/import endpoints, lock pitfall, --body semantics`.
Never `--no-verify` unless the guard fails for a quotable reason; disclose it in the body
if you must. Do NOT push. Report the commit SHA and the sections you changed.

## Dispatch note (added by foreman)

This repo's gitreins pre-commit guard can SILENTLY NO-OP on non-Go diffs (exit 0, but
no commit is created — see board row DF-046). After `git commit`, ALWAYS verify with
`git log -1 --format=%H` that a NEW commit exists. If HEAD did not move, retry the
commit once; if it no-ops again, commit with `--no-verify` and disclose the bypass in
the commit message body (this is the sanctioned path for a silent guard no-op).
