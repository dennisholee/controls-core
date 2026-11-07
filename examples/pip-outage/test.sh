#!/usr/bin/env bash
# Contract test: when the PIP is unreachable, attribute-backed reads must fail
# closed before the upstream is called.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
COMPOSE="docker compose -f compose.yaml"
BASE=http://127.0.0.1:18002
fail() { echo "SAMPLE FAIL: $*" >&2; $COMPOSE logs >&2 || true; exit 1; }
cleanup() { $COMPOSE down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT

code=""
for _ in $(seq 1 60); do
  code=$(curl -s -o /tmp/outage-body.$$ -w '%{http_code}' -H 'x-subject: u-1' "$BASE/accounts/A123" 2>/dev/null || true)
  [ "$code" = "503" ] && break
  sleep 1
done
body=$(cat /tmp/outage-body.$$ 2>/dev/null || true)
rm -f /tmp/outage-body.$$
[ "$code" = "503" ] || fail "expected 503 with PIP down, got $code: $body"
grep -q 'pip_unavailable' <<<"$body" || fail "wrong reason: $body"

if $COMPOSE logs app 2>/dev/null | grep -q 'upstream received'; then
  fail "upstream was called despite PIP outage"
fi
published=$($COMPOSE port app 8080 2>/dev/null || true)
case "$published" in
  *0.0.0.0:*|*"[::]":*|*127.0.0.1:*) fail "upstream app is published directly: $published" ;;
esac

echo "SAMPLE OK: PIP outage returns 503, upstream never called"
