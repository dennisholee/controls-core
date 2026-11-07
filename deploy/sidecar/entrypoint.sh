#!/usr/bin/env bash
set -euo pipefail
: "${PIP_URL:?PIP_URL is required}"
/usr/local/bin/extproc \
  -listen :50051 \
  -config /etc/controls/bundle \
  -pip "$PIP_URL" \
  -metrics-addr :9090 \
  -audit-salt "${CONTROLS_AUDIT_SALT:-sample-salt}" \
  -audit-buffer 0 \
  -allow-unsigned &
ext=$!
trap 'kill "$ext" 2>/dev/null || true' TERM INT
envoy -c /etc/controls/envoy.yaml \
  --service-cluster "${SERVICE_NAME:-sample}" \
  --service-node "$(hostname)" &
env=$!
wait -n "$ext" "$env"
