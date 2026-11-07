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

# Payment write: entitlement + DQ + rejected fields.
post() { curl -s -o /tmp/spring-payment.$$ -w '%{http_code}' -H 'content-type: application/json' -H "x-subject: $1" -d "$2" "$BASE/payments"; }
pbody() { cat /tmp/spring-payment.$$; rm -f /tmp/spring-payment.$$; }
code=$(post u-1 '{"_type":"payment","_id":"P-9001","amount":10,"currency":"USD"}')
[ "$code" = "201" ] || { pbody; fail "valid payment got $code"; }
code=$(post u-2 '{"_type":"payment","_id":"P-9001","amount":10,"currency":"USD"}')
[ "$code" = "403" ] || { pbody; fail "non-owner got $code"; }
code=$(post u-1 '{"_type":"payment","_id":"P-9001","amount":-1,"currency":"USD"}')
[ "$code" = "403" ] || { pbody; fail "negative amount got $code"; }
pbody | grep -q 'dq_blocking:payment.amount.positive' || fail "wrong DQ reason"
code=$(post u-1 '{"_type":"payment","_id":"P-9001","amount":10,"currency":"USD","pan":"4111111111111111"}')
[ "$code" = "403" ] || { pbody; fail "PAN got $code"; }
pbody | grep -q 'rejected_field:pan' || fail "wrong PAN reason"

echo "SAMPLE OK: entitlement + DQ + OpenAPI gate"
