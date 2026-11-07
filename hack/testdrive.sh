#!/usr/bin/env bash
# Non-interactive local bootstrap check: starts the test API, evaluates one
# request, asserts masking, and exits. No Docker required.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${TESTDRIVE_PORT:-18081}"
TMP="$(mktemp -d)"
PIDS=()
cleanup() {
  for p in "${PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$TMP"
}
trap cleanup EXIT
fail() { echo "TESTDRIVE FAIL: $*" >&2; exit 1; }

command -v go >/dev/null || fail "go is not on PATH"
export GOCACHE="${GOCACHE:-$ROOT/.gocache}"
export GOPATH="${GOPATH:-$ROOT/.gopath}"
export GOPROXY="${GOPROXY:-off}"

echo "building sidecar harness"
(cd "$ROOT" && go build -o "$TMP/sidecar" ./sidecar/cmd/sidecar) || fail "build failed"

echo "starting test API on 127.0.0.1:$PORT"
"$TMP/sidecar" -addr "127.0.0.1:$PORT" \
  -config "$ROOT/integration/testdata/config" \
  -fixtures "$ROOT/pip/fixtures/attributes.json" >"$TMP/api.log" 2>&1 &
PIDS+=($!)
disown "$!" 2>/dev/null || true

for _ in $(seq 1 50); do
  if curl -sf "http://127.0.0.1:$PORT/readyz" >/dev/null; then break; fi
  sleep 0.1
done
curl -sf "http://127.0.0.1:$PORT/readyz" >/dev/null || { tail -20 "$TMP/api.log" >&2; fail "API did not become ready"; }

cat >"$TMP/request.json" <<'JSON'
{
  "request": {"method":"GET","path":"/accounts/A123",
              "headers":{"x-subject":"u-1","x-claim-region":"US"}},
  "response": {"status":200,"body":{
    "accountNumber":"1234567890",
    "holder":{"email":"jane.doe@example.com"},
    "ssn":"111-22-3333"}}
}
JSON

curl -sf "http://127.0.0.1:$PORT/v1/process" -H 'content-type: application/json' \
  --data-binary "@$TMP/request.json" >"$TMP/result.json" || fail "process call failed"

python3 - "$TMP/result.json" <<'PY'
import json, sys
r = json.load(open(sys.argv[1]))
d = r["decision"]
if not d.get("allowed"):
    raise SystemExit(f"unexpected deny: {d}")
body = r["response"]["body"]
if body.get("ssn") != "[REDACTED]":
    raise SystemExit(f"ssn not masked: {body}")
if body.get("accountNumber") != "******7890":
    raise SystemExit(f"accountNumber masking wrong: {body}")
print(json.dumps(r, indent=2))
PY

echo
echo "TESTDRIVE OK — masked read allowed, ssn redacted"
echo "Interactive console: make api  (http://127.0.0.1:8081/docs)"
echo "Live Envoy smoke:    make smoke (requires Docker)"
