#!/usr/bin/env bash
set -euo pipefail
: "${PIP_URL:?PIP_URL is required}"
args=(
  -listen :50051
  -config /etc/controls/bundle
  -pip "$PIP_URL"
  -metrics-addr :9090
  -audit-salt "${CONTROLS_AUDIT_SALT:-sample-salt}"
  -audit-buffer 0
  -allow-unsigned
)
if [ -n "${PIP_CA:-}" ]; then
  args+=(-pip-ca "$PIP_CA")
fi
if [ -n "${PIP_CERT:-}" ]; then
  args+=(-pip-cert "$PIP_CERT")
fi
if [ -n "${PIP_KEY:-}" ]; then
  args+=(-pip-key "$PIP_KEY")
fi
if [ -n "${PIP_SERVER_NAME:-}" ]; then
  args+=(-pip-server-name "$PIP_SERVER_NAME")
fi
/usr/local/bin/extproc "${args[@]}" &
ext=$!
trap 'kill "$ext" 2>/dev/null || true' TERM INT
envoy -c /etc/controls/envoy.yaml \
  --service-cluster "${SERVICE_NAME:-sample}" \
  --service-node "$(hostname)" &
env=$!
wait -n "$ext" "$env"
