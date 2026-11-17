# Task DF-033 — `--data-dir` must not be silently overridden by dashboard routing

You are a coding worker. Repo worktree: `/home/kara/worktrees/musterflow-DF-033` (cd there first; do
ALL work there — never edit `/home/kara/musterflow`). Branch: `wt/DF-033`.

## Row (verbatim)
"P2 — Silent dashboard-routing override breaks --data-dir isolation when server is running: with
dashboard on :9876, CLI ignores --data-dir for ALL registry ops and routes to the server's registry,
returning SUCCESS while writing nothing to the requested dir. Verified 2026-09-10: import to fresh
data2 listed 1 API while local-mode truth showed data2 empty (API actually upserted into the server's
registry). Broken round-trip pattern for multi-env workflows. FIX: warn loudly when --data-dir
differs from the routed server's data-dir, or disable routing when an explicit --data-dir is given.
PASS: import --data-dir fresh-dir with server running either writes that dir (verifiable via
--no-dashboard list) or errors clearly."

## Root cause (pre-located — verify, then fix)
`cmd/musterflow/main.go` lines ~57-110:

```go
dashRunning := false
if !flagNoDashboard {
    dashRunning = isMusterflowDashboard(dashAddr)
}
registry := app.NewRegistry(cfg.DataDir)
if dashRunning {
    cli.SetDashboardURL(fmt.Sprintf("http://%s", dashAddr))   // every registry op now routes to the SERVER
} else {
    if err := registry.Load(); err != nil { ... }
}
```

`cfg` is loaded with the `--data-dir` value, so the requested dir is known, but once
`dashRunning` is true every registry command routes through the dashboard HTTP API and silently
writes into the SERVER's data dir. A user who explicitly passed `--data-dir` gets a success message
for an operation that never touched the dir they named.

Note the existing adjacent work you must not regress: DF-045 already made a store-load failure
fail-closed with an actionable lock message (`cli.SetStoreError`, `app.AsStoreLock`) and suppressed
the raw `Warning: could not load registry:` line for lock conflicts. There are tests around that
(`cmd/musterflow/main_test.go`, `internal/cli/storeerr_test.go`, `internal/cli/errorpath_test.go`) —
keep them green.

## Fix (pick the minimal, honest shape)
**Preferred**: an explicit `--data-dir` wins. When `flagDataDir != ""` and the dashboard is running,
do NOT route: load the local registry from the requested dir and print ONE clear stderr line naming
both dirs, e.g.

```
note: --data-dir /tmp/env2 given, so dashboard routing at http://127.0.0.1:9876 is disabled;
      the running dashboard keeps its own registry
```

**Acceptable alternative** (only if the preferred shape breaks unrelated tests): keep routing but
emit a loud stderr warning that names the server's data dir and the requested one, and say the
operation writes to the server's registry, not the requested dir.
Either way: never surface a raw DuckDB `IO Error` and never report bare success for a write that went
somewhere else.

Preserve: `--no-dashboard` behaviour, `--dashboard-addr` detection, the DF-013
`isMusterflowDashboard` health probe, the DF-045 lock messaging, and the default path when no
`--data-dir` is passed (dashboard routing stays ON — that is the documented multi-process contract).

**Do NOT edit** `README.md`, `docs/**`, `go.mod`, `go.sum`, the `.coding-hermes/` board, or
`.gitreins/`.

## Acceptance criteria
Row PASS shape: with the dashboard running, `import --data-dir <fresh-dir> <file>` either writes that
dir (verifiable with `--no-dashboard list --data-dir <fresh-dir>`) or errors clearly. You must
demonstrate one of those two, plus the default no-`--data-dir` routing still works.

## Verification you must run and paste
1. **Red-proof first.** Build the PRE-FIX binary from the base commit and show the bug is real on it:
   ```
   git stash list   # expect empty
   git worktree add /tmp/mf-df033-prefix 8c488ed      # if that fails, use: git archive 8c488ed | tar -x -C /tmp/mf-df033-prefix-src
   (cd /tmp/mf-df033-prefix-src && go build -o /tmp/mf-df033-prefix-bin ./cmd/musterflow)
   ```
   Then, with YOUR OWN dashboard instance on a scratch port and scratch data dir, run the row's exact
   scenario on the pre-fix binary and capture that the fresh dir stays empty. Never probe or kill the
   live `:9876` service; use `--dashboard-addr 127.0.0.1:19833` plus a scratch `--data-dir`.
2. Post-fix binary: same scenario passes (dir written, then `--no-dashboard list --data-dir` shows the
   imported API; or the clear error path).
3. `gofmt -l cmd internal` → empty. `go build ./...`, `go vet ./...` → clean.
4. `go test -count=1 ./cmd/... ./internal/cli/` → pass; then the full `go test -count=1 ./...` → pass.
5. Kill every server/binary you started; end with the scratch ports free.

## Commit rules
- `unset GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL` before committing.
- One commit: `fix(DF-033): explicit --data-dir disables dashboard routing`
  (body: the two-dir hazard, the chosen shape, and the red-proof result).
- Repo hook may append a `Co-authored-by:` trailer — expected, do not amend.
- **Do NOT push.** No board/DuckBrain writes.

## Report
The pre-fix failure evidence, the post-fix AC transcript, chosen shape and why, test results,
commit SHA, and any behaviour you intentionally left unchanged.
