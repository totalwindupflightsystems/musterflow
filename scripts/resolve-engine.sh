#!/usr/bin/env bash
# resolve-engine.sh — THE single mechanism for resolving the muster engine.
#
# The engine (github.com/wojons/muster) is a private local dependency. This
# script generates a Go workspace (go.work) pointing at an engine checkout via
# a workspace-level replace directive, so the module graph resolves WITHOUT
# any replace in go.mod:
#   - $MUSTER_ENGINE_DIR   explicit override (any location)
#   - ../muster            sibling checkout (local dev, fresh clones)
#   - ./muster             in-repo checkout (CI, docker build context)
#
# `use` alone is NOT enough: the module graph still fetches the required
# version's go.mod from the cache/proxy (impossible for a private engine), so
# go.work carries the replace. It NEVER edits go.mod or go.sum. Idempotent:
# re-running writes a byte-identical go.work and leaves go.mod/go.sum
# untouched.
#
# Usage: bash scripts/resolve-engine.sh   (from the repo root)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ ! -f go.mod ]]; then
  echo "error: go.mod not found — run from the repo root" >&2
  exit 1
fi

ENGINE=""
if [[ -n "${MUSTER_ENGINE_DIR:-}" && -f "${MUSTER_ENGINE_DIR}/go.mod" ]]; then
  ENGINE="$MUSTER_ENGINE_DIR"
elif [[ -d ../muster && -f ../muster/go.mod ]]; then
  ENGINE="../muster"
elif [[ -d ./muster && -f ./muster/go.mod ]]; then
  ENGINE="./muster"
fi

if [[ -z "$ENGINE" ]]; then
  echo "error: muster engine not found — set MUSTER_ENGINE_DIR or clone it as" >&2
  echo "       ../muster (sibling) or ./muster (CI/docker build context)" >&2
  exit 1
fi

# go.work go directive = highest go directive among the main module and the
# engine (keeps the workspace resolvable when the engine bumps its go
# directive — see GO-TOOLCHAIN-001).
GO_VERSION="$(awk '/^go /{print $2; exit}' go.mod)"
ENGINE_GO_VERSION="$(awk '/^go /{print $2; exit}' "$ENGINE/go.mod" 2>/dev/null || true)"
if [[ -n "$ENGINE_GO_VERSION" ]]; then
  GO_VERSION="$(printf '%s\n%s\n' "$GO_VERSION" "$ENGINE_GO_VERSION" | sort -V | tail -n 1)"
fi

ENGINE_MODULE="github.com/wojons/muster"

cat > go.work <<EOF
go $GO_VERSION

use .

replace ${ENGINE_MODULE} => $ENGINE
EOF

echo "muster engine resolved to ${ENGINE} (go.work written, go.mod untouched)"
