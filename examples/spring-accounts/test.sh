#!/usr/bin/env bash
# Contract test for the accounts-read sample. Fails when the sidecar stops
# masking, stops denying, or exposes the upstream directly.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
COMPOSE="docker compose -f compose.yaml"
BASE=http://127.0.0.1:18005
fail() { echo "SAMPLE FAIL: $*" >&2; $COMPOSE logs >&2 || true; exit 1; }
cleanup() { $COMPOSE down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT

for _ in $(seq 1 60); do
  if curl -sf -H 'x-subject: u-1' "$BASE/accounts/A123" >/dev/null 2>&1; then break; fi
  sleep 1
done

allow=$(curl -s -H 'x-subject: u-1' "$BASE/accounts/A123") || fail "allow request failed"
python3 - "$allow" <<'PY'
import json, sys
body = json.loads(sys.argv[1])
assert body["ssn"] == "[REDACTED]", body
assert body["accountNumber"] == "******7890", body
assert body["holder"]["email"] == "j***@example.com", body
PY

code=$(curl -s -o /tmp/sample-deny.json -w '%{http_code}' -H 'x-subject: u-2' "$BASE/accounts/A123")
[ "$code" = "403" ] || fail "expected 403 for u-2, got $code"
grep -q 'default:deny' /tmp/sample-deny.json || fail "unexpected deny reason: $(cat /tmp/sample-deny.json)"

code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/accounts/A123")
[ "$code" = "403" ] || fail "unauthenticated call should be 403, got $code"

published=$($COMPOSE port app 8080 2>/dev/null || true)
case "$published" in
  *0.0.0.0:*|*"[::]":*|*127.0.0.1:*) fail "upstream app is published directly: $published" ;;
esac

echo "SAMPLE OK: masked allow, 403 deny, 403 unauthenticated, upstream not published"
