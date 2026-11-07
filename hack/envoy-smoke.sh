#!/usr/bin/env bash
# Live Envoy smoke test: real Envoy -> real ext_proc gRPC service -> engine,
# against a fixture PIP and a fixture upstream app.
#
# Requires: bash, curl, python3, go, Docker.
#
#   make smoke
#   ENVOY_IMAGE=envoyproxy/envoy:v1.40.0 make smoke
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export GOCACHE="${GOCACHE:-$ROOT/.gocache}"
export GOPATH="${GOPATH:-$ROOT/.gopath}"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/controls-smoke.XXXXXX")"
NAME="controls-envoy-smoke-$$"
IMAGE="${ENVOY_IMAGE:-envoyproxy/envoy:v1.40.0@sha256:459f2a6404c340fe6afccd69d85527012d47f3c036765493e6f8f3113fa384de}"
ENVOY_PORT=18000
APP_PORT=18080
PIP_PORT=18082
EXTPROC_PORT=15051
PIDS=()

cleanup() {
  for pid in "${PIDS[@]:-}"; do kill "$pid" 2>/dev/null || true; done
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  rm -rf "$TMP"
}
trap cleanup EXIT

fail() {
  echo "SMOKE FAILED: $*" >&2
  echo "--- extproc log ---" >&2; tail -30 "$TMP/extproc.log" >&2 || true
  echo "--- envoy log ---" >&2; docker logs "$NAME" 2>&1 | tail -40 >&2 || true
  exit 1
}

wait_for() {
  local url=$1
  for _ in $(seq 1 100); do
    if curl -s -o /dev/null --max-time 1 "$url"; then return 0; fi
    sleep 0.2
  done
  return 1
}

echo "building binaries"
(cd "$ROOT" && go build -o "$TMP/extproc" ./sidecar/cmd/extproc)
(cd "$ROOT" && go build -o "$TMP/pip" ./pip/cmd/pip)
(cd "$ROOT" && go build -o "$TMP/app" ./hack/smokeapp)

echo "starting pip, app, extproc"
"$TMP/pip" -addr "127.0.0.1:$PIP_PORT" -fixtures "$ROOT/pip/fixtures/attributes.json" >"$TMP/pip.log" 2>&1 &
PIDS+=($!)
"$TMP/app" -addr ":$APP_PORT" >"$TMP/app.log" 2>&1 &
PIDS+=($!)
"$TMP/extproc" -listen ":$EXTPROC_PORT" -config "$ROOT/integration/testdata/config" \
  -pip "http://127.0.0.1:$PIP_PORT" -metrics-addr :19090 -audit-sample 1 -audit-salt smoke -allow-unsigned -audit-buffer 0 \
  >"$TMP/extproc.log" 2>&1 &
PIDS+=($!)

wait_for "http://127.0.0.1:$PIP_PORT/healthz" || fail "pip did not start"
wait_for "http://127.0.0.1:$APP_PORT/healthz" || fail "app did not start"
for _ in $(seq 1 50); do
  if (exec 3<>"/dev/tcp/127.0.0.1/$EXTPROC_PORT") 2>/dev/null; then exec 3>&-; break; fi
  sleep 0.1
done

echo "starting envoy ($IMAGE)"
if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker pull "$IMAGE"
fi
EXTRA=()
if [ "$(uname -s)" = "Linux" ]; then
  EXTRA+=(--add-host=host.docker.internal:host-gateway)
fi
docker run -d --name "$NAME" -p "$ENVOY_PORT:8080" ${EXTRA[@]+"${EXTRA[@]}"} \
  -v "$ROOT/hack/envoy-smoke.yaml:/etc/envoy/envoy.yaml:ro" \
  "$IMAGE" -c /etc/envoy/envoy.yaml --log-level warn >/dev/null

echo "waiting for envoy"
ready=0
for _ in $(seq 1 100); do
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 \
    -H 'x-subject: u-1' -H 'x-claim-region: US' \
    "http://127.0.0.1:$ENVOY_PORT/accounts/A123" || true)
  if [ "$code" = "200" ]; then ready=1; break; fi
  sleep 0.3
done
[ "$ready" = "1" ] || fail "envoy did not become ready (last status $code)"

check_json() {
  local file=$1
  shift
  python3 - "$file" "$@" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1]))
for expr in sys.argv[2:]:
    path, want = expr.split("=", 1)
    cur = doc
    for part in path.split("."):
        cur = cur[part]
    if str(cur) != want:
        raise SystemExit(f"{path}: got {cur!r}, want {want!r}")
PY
}

status_and_body() {
  local out=$1
  shift
  curl -s -o "$out.body" -w '%{http_code}' "$@"
}

# 1. Authorized read: payload reaches the app, response is masked.
code=$(status_and_body "$TMP/allow" -H 'x-subject: u-1' -H 'x-claim-region: US' \
  "http://127.0.0.1:$ENVOY_PORT/accounts/A123")
[ "$code" = "200" ] || fail "authorized read status $code"
check_json "$TMP/allow.body" \
  'accountNumber=******7890' 'ssn=[REDACTED]' 'holder.email=j***@example.com' 'region=US' \
  || fail "authorized read not masked"

# 2. Unauthorized read: denied before the upstream is reached.
code=$(status_and_body "$TMP/deny" -H 'x-subject: u-2' -H 'x-claim-region: EU' \
  "http://127.0.0.1:$ENVOY_PORT/accounts/A123")
[ "$code" = "403" ] || fail "unauthorized read status $code"

# 3. Self-description mismatch on a write.
code=$(status_and_body "$TMP/mismatch" -H 'content-type: application/json' \
  -H 'x-subject: u-1' -d '{"_type":"account","_id":"P-9001","amount":10,"ownerId":"u-1"}' \
  "http://127.0.0.1:$ENVOY_PORT/payments")
[ "$code" = "403" ] || fail "self-description mismatch status $code"

# 4. Authorized write reaches the app.
code=$(status_and_body "$TMP/write" -H 'content-type: application/json' \
  -H 'x-subject: u-1' \
  -d '{"_type":"payment","_id":"P-9001","amount":10,"currency":"USD","ownerId":"u-1"}' \
  "http://127.0.0.1:$ENVOY_PORT/payments")
[ "$code" = "201" ] || fail "authorized write status $code"

# 5. Unregistered route is denied.
code=$(status_and_body "$TMP/unreg" -H 'x-subject: u-1' \
  "http://127.0.0.1:$ENVOY_PORT/admin/secrets")
[ "$code" = "403" ] || fail "unregistered route status $code"

# 6. The same acceptance criteria the engine suite runs, but through Envoy.
#    This is the end-to-end verification: real ext_proc, real upstream, real
#    masking. Engine-only scenarios are skipped by the HTTP backend.
echo "running BDD acceptance criteria against envoy"
(cd "$ROOT" && CONTROLS_ACCEPTANCE_URL="http://127.0.0.1:$ENVOY_PORT" \
  go test ./integration -run TestAcceptance -count=1 -v) || fail "BDD acceptance criteria failed"

# 7. Audit and metrics are emitted for the traffic above.
echo "checking audit and metrics"
curl -sf "http://127.0.0.1:19090/healthz" >/dev/null || fail "metrics healthz failed"
curl -sf "http://127.0.0.1:19090/metrics" >"$TMP/metrics" || fail "metrics endpoint failed"
grep -q 'controls_decisions_total' "$TMP/metrics" || fail "decisions metric missing"
grep -q 'controls_pip_cache_total' "$TMP/metrics" || fail "pip cache metric missing"
grep -q '"subjectHash"' "$TMP/extproc.log" || fail "audit log missing"
grep -q '"decision":"deny"' "$TMP/extproc.log" || fail "audit deny missing"
curl -sf "http://127.0.0.1:9090/metrics" >"$TMP/pip-metrics" || fail "PIP metrics endpoint failed"
grep -q 'pip_requests_total' "$TMP/pip-metrics" || fail "PIP requests metric missing"

# 8. Optional throughput/latency gate through the real Envoy chain.
if [ "${CONTROLS_LOADTEST:-0}" = "1" ]; then
  LOADTEST_RPS="${LOADTEST_RPS:-1000}"
  LOADTEST_DURATION="${LOADTEST_DURATION:-10s}"
  LOADTEST_P99="${LOADTEST_P99:-100ms}"
  LOADTEST_CONCURRENCY="${LOADTEST_CONCURRENCY:-100}"
  echo "running load test (${LOADTEST_RPS} rps, ${LOADTEST_DURATION})"
  (cd "$ROOT" && go build -o "$TMP/loadtest" ./hack/loadtest)
  "$TMP/loadtest" -url "http://127.0.0.1:$ENVOY_PORT/accounts/A123" \
    -H 'x-subject: u-1' -H 'x-claim-region: US' \
    -duration "$LOADTEST_DURATION" -concurrency "$LOADTEST_CONCURRENCY" \
    -rps "$LOADTEST_RPS" -max-p99 "$LOADTEST_P99"
fi

echo "SMOKE OK: allow+mask, deny, self-description mismatch, write, unregistered, BDD, audit+metrics"
