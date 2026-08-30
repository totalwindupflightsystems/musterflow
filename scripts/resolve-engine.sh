#!/usr/bin/env bash
# resolve-engine.sh — THE single mechanism for resolving the muster engine replace.
#
# The engine (github.com/wojons/muster) is a local dependency pinned via a
# `replace` directive in go.mod. This script rewrites ONLY that single replace
# line so the module graph resolves against the right checkout:
#   - ./muster          when an engine checkout is present in the repo dir (CI/docker)
#   - /home/kara/muster otherwise (local dev)
#
# Idempotent: when the replace line already points at the resolved target,
# go.mod stays byte-identical. Never touches require/indirect blocks or go.sum.
#
# Usage: bash scripts/resolve-engine.sh   (from the repo root)
set -euo pipefail

if [[ ! -f go.mod ]]; then
  echo "error: go.mod not found — run from the repo root" >&2
  exit 1
fi

if [[ -d ./muster ]]; then
  TARGET='./muster'
else
  TARGET='/home/kara/muster'
fi

if grep -q '^replace github.com/wojons/muster =>' go.mod; then
  sed -i "s|^replace github.com/wojons/muster => .*|replace github.com/wojons/muster => ${TARGET}|" go.mod
else
  echo "replace github.com/wojons/muster => ${TARGET}" >> go.mod
fi

echo "muster engine replace resolved to: ${TARGET}"
