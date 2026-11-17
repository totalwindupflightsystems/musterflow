#!/usr/bin/env bash
# resolve-engine.sh — OPTIONAL local-engine (workspace) resolution.
#
# The muster engine (github.com/wojons/muster) is a published Go module: go.mod
# requires a proxy-resolvable version, so a fresh clone builds with plain
# `go build ./...` and NO engine checkout. This script exists only for engine
# DEVELOPMENT — it generates a Go workspace (go.work) with a workspace-level
# replace directive pointing at a local engine checkout, so your edits to the
# engine are used instead of the published module:
#   - $MUSTER_ENGINE_DIR   explicit override (any location)
#   - ../muster            sibling checkout (local dev)
#   - ./muster             in-repo checkout (CI, docker build context)
#
# `use` alone is NOT enough: the module graph still resolves the engine's own
# requirements through the workspace, so go.work carries the replace. It NEVER
# edits go.mod or go.sum. Idempotent: re-running writes a byte-identical
# go.work and leaves go.mod/go.sum untouched.
#
# When NO engine checkout is found this is NOT an error (the published module
# covers installs): it exits 0 and, if a leftover go.work points at a checkout
# that no longer exists, removes it — a dangling replace breaks every go
# command, which is what used to break fresh clones.
#
# Usage: bash scripts/resolve-engine.sh   (from the repo root)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

ENGINE_MODULE="github.com/wojons/muster"

if [[ ! -f go.mod ]]; then
  echo "error: go.mod not found — run from the repo root" >&2
  exit 1
fi

# Engine directory go.work currently points at (empty when absent/no replace).
work_target() {
  [[ -f go.work ]] || return 0
  awk -v mod="$ENGINE_MODULE" '$1 == "replace" && $2 == mod { print $4; exit }' go.work
}

ENGINE=""
if [[ -n "${MUSTER_ENGINE_DIR:-}" && -f "${MUSTER_ENGINE_DIR}/go.mod" ]]; then
  ENGINE="$MUSTER_ENGINE_DIR"
elif [[ -d ../muster && -f ../muster/go.mod ]]; then
  ENGINE="../muster"
elif [[ -d ./muster && -f ./muster/go.mod ]]; then
  ENGINE="./muster"
fi

if [[ -z "$ENGINE" ]]; then
  STALE="$(work_target)"
  if [[ -n "$STALE" && -d "$STALE" ]]; then
    # An existing workspace still points at a live checkout — leave it alone.
    echo "no engine in the standard locations; existing go.work kept (engine: $STALE)"
    exit 0
  fi
  if [[ -n "$STALE" ]]; then
    rm -f go.work go.work.sum
    echo "info: no local muster engine checkout — removed stale go.work ($STALE is gone);" \
      "building against the published muster module (set MUSTER_ENGINE_DIR to develop against a local checkout)"
    exit 0
  fi
  echo "info: no local muster engine checkout — building against the published muster module" \
    "(set MUSTER_ENGINE_DIR to develop against a local checkout)"
  exit 0
fi

# go.work go directive = highest go directive among the main module and the
# engine (keeps the workspace resolvable when the engine bumps its go
# directive — see GO-TOOLCHAIN-001).
GO_VERSION="$(awk '/^go /{print $2; exit}' go.mod)"
ENGINE_GO_VERSION="$(awk '/^go /{print $2; exit}' "$ENGINE/go.mod" 2>/dev/null || true)"
if [[ -n "$ENGINE_GO_VERSION" ]]; then
  GO_VERSION="$(printf '%s\n%s\n' "$GO_VERSION" "$ENGINE_GO_VERSION" | sort -V | tail -n 1)"
fi

cat > go.work <<EOF
go $GO_VERSION

use .

replace ${ENGINE_MODULE} => $ENGINE
EOF

echo "muster engine resolved to ${ENGINE} (go.work written, go.mod untouched)"
