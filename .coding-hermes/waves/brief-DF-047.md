# Task DF-047 — CI docker job must build the same module graph as the documented compose path

You are a coding worker. Repo worktree: `/home/kara/worktrees/musterflow-DF-047` (cd there first; do ALL
work there — never edit `/home/kara/musterflow`, that is the foreman's checkout). Branch: `wt/DF-047`.

## Environment facts (verified by the foreman this tick)
- Go module `github.com/totalwindupflightsystems/musterflow`, Go 1.26.x. The engine
  `github.com/wojons/muster` is a PUBLISHED module pinned in `go.mod`
  (`v0.1.2-0.20261117020333-c64dc501ec58`) with **no `replace` directive**, so plain
  `go build ./...` works from a cold shell. There is no local engine checkout requirement.
- `Dockerfile` (already correct) documents the contract: the image builds from this repo's context
  alone; `scripts/resolve-engine.sh` is informational inside the image and never edits `go.mod`;
  there is deliberately no extra build context.
- CI status at tick start: `ci` and `docker` both **green** on `8c488ed`. Do not regress them.

## The defect
`.github/workflows/docker.yml` (lines 24-38) still contains:

```yaml
      - name: Checkout muster engine
        uses: actions/checkout@v4
        with:
          repository: wojons/muster
          path: muster
          token: ${{ secrets.GH_PAT }}

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - name: Resolve muster engine replace
        run: bash scripts/resolve-engine.sh
```

Consequences: the CI docker job builds a **different module graph** than the path the README
documents (`docker compose up -d --build`, which resolves the published module), it needs
`secrets.GH_PAT` to read a repo it no longer needs, and the whole engine checkout plus any generated
`go.work` sits in the build context that the Dockerfile's `COPY . .` pulls into the builder — exactly
hiding a regression that breaks the documented path.

## Fix (scoped — one file)
Delete the `Checkout muster engine` step and the `Resolve muster engine replace` step from
`.github/workflows/docker.yml`. Keep everything else intact, including the `Set up Go` step,
`Install dependencies` (`go mod download`), `Build static binary`, QEMU/buildx setup, and the GHCR
login that still uses `${{ secrets.GH_PAT }}` (registry auth is unrelated to the engine checkout).

Replace them with nothing, or with a short YAML comment stating why the docker job intentionally has
no engine checkout (parity with `docker compose up -d --build`).

**Do NOT touch**: `.github/workflows/ci.yml`, `Dockerfile`, `docker-compose.yml`,
`scripts/resolve-engine.sh`, `go.mod`, `go.sum`, `README.md`, the board under `.coding-hermes/`.

## Acceptance criteria (row, verbatim)
"CI's docker job builds a different module graph than the documented compose path (./muster + go.work
leak into the image context)" — the fix must leave the docker job building **only** the
published-module graph, with no `wojons/muster` checkout, no `./muster` path in the repo context, and
no `resolve-engine.sh` invocation.

## Verification you must run and paste output for
1. `grep -n 'muster\|resolve-engine\|build-contexts\|secrets.GH_PAT' .github/workflows/docker.yml`
   — must show no `repository: wojons/muster`, no `path: muster`, no `resolve-engine.sh`; the only
   `GH_PAT` left is the GHCR login.
2. YAML parses: `python3 -c "import yaml;d=yaml.safe_load(open('.github/workflows/docker.yml'));print([s.get('name') for s in d['jobs']['build']['steps']])"`
   (if python is unavailable, `ruby -ryaml -e 'p YAML.load_file(".github/workflows/docker.yml")["jobs"]["build"]["steps"].map{|s| s["name"]}'`).
3. `go build ./...` and `go vet ./...` still pass (proves the tree is otherwise clean).
4. `git diff --stat` touches exactly ONE file.

## Commit rules
- `unset GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL` before committing.
- One commit: `ci(DF-047): docker workflow builds the published-module graph only`
  with a body explaining the parity argument (compose path vs CI job).
- The repo's pre-commit hook may append a `Co-authored-by:` trailer — that is expected; do not amend.
- **Do NOT push.** Do not open a PR. Do not update the board, tasks.yaml, or DuckBrain.
- If the hook silently creates no commit (known gitreins behavior on some diffs), confirm with
  `git log -1 --oneline` and retry once before reporting.

## Report (final message)
Files changed, the grep output, the step-name list, `go build`/`go vet` result, commit SHA, and any
residual risk (e.g. whether ci.yml should get the same treatment — report, do not change).
