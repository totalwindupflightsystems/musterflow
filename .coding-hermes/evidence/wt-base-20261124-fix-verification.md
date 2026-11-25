# WT-BASE-20261124 — fix verification pack

Fix location is fleet infrastructure, NOT in this repo: `/home/kara/.hermes/scripts/worktree.sh`
(`~/.hermes/scripts` is not git-tracked — verified `git rev-parse --show-toplevel` fails there).
The verbatim applied patch, gate output and live probes are recorded below.

## Applied patch (exact)

```diff
 default_base() {
   local repo="$1" ref
-  for ref in origin/main origin/master main master; do
+  # Live default branch first: origin/HEAD is what the remote actually declared.
+  # (WT-BASE-20261124: hard origin/main preference cut musterflow worktrees from a
+  # month-old stale origin/main in repos holding BOTH origin/main and origin/master.)
+  if ref="$(git -C "$repo" symbolic-ref --quiet refs/remotes/origin/HEAD 2>/dev/null)"; then
+    printf '%s' "${ref#refs/remotes/}"; return
+  fi
+  for ref in origin/master origin/main main master; do
     if git -C "$repo" rev-parse --verify --quiet "$ref" >/dev/null; then printf '%s' "$ref"; return; fi
   done
   die "no main/master ref found in $repo"
 }
```

## GATE ARM (row acceptance criterion 3)

`bash -n /home/kara/.hermes/scripts/worktree.sh` → exit code **0** (run 2026-11-25 ~16:00Z).

## AC1 verification — live probes

1. musterflow repo, which HOLDS BOTH refs: `origin/main` exists AND `symbolic-ref refs/remotes/origin/HEAD` = `refs/remotes/origin/master`.
   Extracted `default_base()` run against it returned **`origin/master`** (before the fix this returned `origin/main` → stale Oct-04 commit 1bbcf7a).
2. Synthetic fixture: bare `origin.git` with both `master` (HEAD) and `main` branches, clone with both remote-tracking refs, `remote.origin.fetch += main`. Extracted `default_base()` run → **`origin/master`** via the new symbolic-ref-first path.

## AC2 (row evidence, pre-verified earlier)

Worktrees mispointed to stale `origin/main` were re-cut from `origin/master` c61bd90 in tick
musterflow-foreman-2026-11-24-19-47-57; recorded in `.coding-hermes/waves/musterflow-foreman-2026-11-24-19-47-57.json`
(`"base": "origin/master", "base_sha": "c61bd90"`).

## Post-fix worktree cut using the fixed script (live on this tick)

`worktree.sh new musterflow REVIEW-MUSTERFLOW-20261111` →
`BASE=origin/master BASE_SHA=afab6d0` — the stale-preference path is gone.
