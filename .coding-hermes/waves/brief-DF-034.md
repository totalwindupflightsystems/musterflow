# Task DF-034 — flag-shape asymmetries: accept `--name` on `flow run`, `--input` on `import`

You are a coding worker. Repo worktree: `/home/kara/worktrees/musterflow-DF-034` (cd there first; do
ALL work there — never edit `/home/kara/musterflow`). Branch: `wt/DF-034`.

## Row (verbatim)
"P2 — Flag-shape asymmetries persist across subcommands (3rd dogfood in a row): `flow run --name X`
→ 'unknown flag: --name' (positional only) while `flow create` accepts both; `import --input <path>`
→ unknown flag (positional only, <path> in usage). Same-verb families accept different flag styles.
FIX: accept --name on flow run and --input/--file on import (or document positional-only in help +
README). PASS: both forms work or help/README states the accepted shape."

## Root cause (pre-located by the foreman — do not re-derive)
- `internal/cli/root.go` ~line 495-520: `runCmd` is `Use: "run <name>"`, `Args: cobra.ExactArgs(1)`,
  `engine.Run(args[0], triggerPayload)`, and registers ONLY `--payload`. There is no `--name` flag.
- SAME FILE, `createCmd` (~line 410-447) is the reference shape: it declares `Args:
  cobra.MaximumNArgs(1)`, resolves the name as flag-then-positional, rejects `conflicting flow names:
  positional %q vs --name %q`, and errors `flow name required (positional or --name)` when neither is
  given. Bind the shared `name` variable to a `runCmd` flag the same way `createCmd` does; make sure
  `flow create` behaviour is unchanged.
- `internal/cli/commands.go` ~line 85-115: `newImportCommand` is `Use: "import <path>"`, `Args:
  cobra.ExactArgs(1)`, uses `args[0]`, and declares no path flag. SAME FILE, `newExportCommand`
  (~line 45-83) is the reference shape: `-o/--output` flag plus positional, flag wins.
  Mirror it for import with `--input` (a `--file` alias is optional; if you add it, both must work).

## Fix requirements
1. `musterflow flow run --name X` and `musterflow flow run X` both work; both with `--payload`.
   Neither given → error `flow name required (positional or --name)`; both given and different →
   `conflicting flow names: positional %q vs --name %q`.
2. `musterflow import --input <path>` and `musterflow import <path>` both work. Neither given →
   clear error naming both forms. A missing file must still produce the existing readable error.
3. Update each command's `Use:`/help text so `--help` shows both accepted forms.
4. Update the dashboard-routed branches too (`flowRunViaDashboard`, `importViaDashboard`) — they must
   receive the resolved name/path, not a raw `args[0]` index (an index on an empty slice panics).
5. Tests: add coverage in `internal/cli/cli_test.go` for (a) `flow run --name`, (b) `flow run`
   positional, (c) neither → error, (d) `import --input`, (e) `import` positional. Follow the file's
   existing test style (in-process cobra execution / existing helpers), do NOT spawn network servers.

**Do NOT edit** `README.md` (a separate doc lane owns it), `go.mod`, `go.sum`, the `.coding-hermes/`
board, or anything under `.gitreins/`.

## Verification you must run and paste
1. `gofmt -l internal/cli` → empty output.
2. `go build ./...` and `go vet ./...` → clean.
3. `go test -count=1 ./internal/cli/` → all pass; then `go test -count=1 ./...` → all pass
   (`-short` is acceptable ONLY for the full sweep if a test needs a live port; report which).
4. Live AC on a FRESH binary, isolated from the real dashboard:
   ```
   go build -o /tmp/mf-df034-bin ./cmd/musterflow
   D=/tmp/mf-df034-data
   /tmp/mf-df034-bin --data-dir $D --no-dashboard flow create --name probe-a --source 'print(1)'
   /tmp/mf-df034-bin --data-dir $D --no-dashboard flow run --name probe-a
   /tmp/mf-df034-bin --data-dir $D --no-dashboard flow run probe-a
   /tmp/mf-df034-bin --data-dir $D --no-dashboard export --output /tmp/mf-df034-reg.jsonl
   /tmp/mf-df034-bin --data-dir $D --no-dashboard import --input /tmp/mf-df034-reg.jsonl
   /tmp/mf-df034-bin --data-dir $D --no-dashboard import /tmp/mf-df034-reg.jsonl
   ```
   Both forms of each must succeed. NEVER run these without `--data-dir` (they would touch
   `~/.musterflow`).

## Commit rules
- `unset GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL` before committing.
- One commit: `fix(DF-034): uniform flag shapes for flow run and import`
  (body: which forms are now accepted, and the reference shape mirrored).
- Repo hook may append a `Co-authored-by:` trailer — expected, do not amend.
- **Do NOT push.** No board/DuckBrain writes.

## Report
Files changed, the AC transcript (both forms), test counts, commit SHA, anything you deliberately
left out (e.g. `--file` alias if you skipped it).
