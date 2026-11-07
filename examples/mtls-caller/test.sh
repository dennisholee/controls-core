#!/usr/bin/env bash
# Contract test: sidecar -> PIP is mutual TLS; masking still works.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
COMPOSE="docker compose -f compose.yaml"
BASE=http://127.0.0.1:18004
fail() { echo "SAMPLE FAIL: $*" >&2; $COMPOSE logs >&2 || true; exit 1; }
cleanup() { $COMPOSE down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT

for _ in $(seq 1 60); do
  if curl -sf -H 'x-subject: u-1' "$BASE/accounts/A123" >/dev/null 2>&1; then break; fi
  sleep 1
done

allow=$(curl -s -H 'x-subject: u-1' "$BASE/accounts/A123") || fail "allow failed"
python3 - "$allow" <<'PY'
import json, sys
body = json.loads(sys.argv[1])
assert body["ssn"] == "[REDACTED]", body
PY

grep -q '"mtls":true' <($COMPOSE logs pip 2>/dev/null) || fail "PIP did not report mTLS"

echo "SAMPLE OK: masked read over mutual TLS"
