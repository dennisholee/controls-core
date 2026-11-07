#!/usr/bin/env bash
# Contract test for the payments-write sample: DQ and rejected fields must
# stop the request before the upstream, and only the owner may write.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
COMPOSE="docker compose -f compose.yaml"
BASE=http://127.0.0.1:18001
fail() { echo "SAMPLE FAIL: $*" >&2; $COMPOSE logs >&2 || true; exit 1; }
cleanup() { $COMPOSE down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT

for _ in $(seq 1 60); do
  if curl -sf -H 'content-type: application/json' -H 'x-subject: u-1'     -d '{"_type":"payment","_id":"P-9001","amount":10,"currency":"USD"}'     "$BASE/payments" >/dev/null 2>&1; then break; fi
  sleep 1
done

post() { curl -s -o /tmp/payment-body.$$ -w '%{http_code}' -H 'content-type: application/json' -H "x-subject: $1" -d "$2" "$BASE/payments"; }
body() { cat /tmp/payment-body.$$; rm -f /tmp/payment-body.$$; }

code=$(post u-1 '{"_type":"payment","_id":"P-9001","amount":10,"currency":"USD"}')
[ "$code" = "201" ] || { body; fail "valid payment got $code"; }

code=$(post u-2 '{"_type":"payment","_id":"P-9001","amount":10,"currency":"USD"}')
[ "$code" = "403" ] || { body; fail "non-owner got $code"; }

code=$(post u-1 '{"_type":"payment","_id":"P-9001","amount":-1,"currency":"USD"}')
[ "$code" = "403" ] || { body; fail "negative amount got $code"; }
body | grep -q 'dq_blocking:payment.amount.positive' || fail "wrong DQ reason"

code=$(post u-1 '{"_type":"payment","_id":"P-9001","currency":"USD"}')
[ "$code" = "403" ] || { body; fail "missing amount got $code"; }
body | grep -q 'dq_blocking:payment.amount.required' || fail "wrong required reason"

code=$(post u-1 '{"_type":"payment","_id":"P-9001","amount":10,"currency":"usd"}')
[ "$code" = "403" ] || { body; fail "bad currency got $code"; }
body | grep -q 'dq_blocking:payment.currency.format' || fail "wrong currency reason"

code=$(post u-1 '{"_type":"payment","_id":"P-9001","amount":10,"currency":"USD","pan":"4111111111111111"}')
[ "$code" = "403" ] || { body; fail "PAN got $code"; }
body | grep -q 'rejected_field:pan' || fail "wrong PAN reason"

published=$($COMPOSE port app 8080 2>/dev/null || true)
case "$published" in
  *0.0.0.0:*|*"[::]":*|*127.0.0.1:*) fail "upstream app is published directly: $published" ;;
esac

echo "SAMPLE OK: write allowed, owner denied, DQ and rejected fields enforced, upstream not published"
