#!/usr/bin/env bash
# Fail when the PIP OpenAPI contract has a breaking change relative to a base
# revision. In CI, pass the target branch, e.g. origin/main.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

BASE="${1:-origin/main}"
if ! git rev-parse --verify "$BASE" >/dev/null 2>&1; then
  if git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    BASE=HEAD~1
  else
    echo "pip-api-compat: no base revision; skipping"
    exit 0
  fi
fi

BIN="${OASDIFF:-}"
if [ -z "$BIN" ]; then
  if [ -x "$ROOT/.tools/bin/oasdiff" ]; then
    BIN="$ROOT/.tools/bin/oasdiff"
  else
    BIN="oasdiff"
  fi
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
git show "$BASE:contracts/pip-api.yaml" >"$tmp"
"$BIN" breaking --fail-on ERR "$tmp" contracts/pip-api.yaml
