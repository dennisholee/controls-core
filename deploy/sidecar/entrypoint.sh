#!/usr/bin/env bash
set -euo pipefail
if [ -n "${CONTROLS_RUNTIME_CONFIG:-}" ]; then
  args=(-runtime-config "$CONTROLS_RUNTIME_CONFIG")
else
  : "${PIP_URL:?PIP_URL is required when CONTROLS_RUNTIME_CONFIG is not set}"
  args=(
    -listen :50051
    -config /etc/controls/bundle
    -pip "$PIP_URL"
    -pip-api-version "${PIP_API_VERSION:-1.0}"
    -metrics-addr :9090
    -audit-salt "${CONTROLS_AUDIT_SALT:-sample-salt}"
    -audit-buffer 0
  )
fi
if [ -z "${CONTROLS_RUNTIME_CONFIG:-}" ]; then
  if [ -n "${CONTROLS_BUNDLE_PUBKEY:-}" ]; then
    args+=(-bundle-pubkey "$CONTROLS_BUNDLE_PUBKEY")
  else
    args+=(-allow-unsigned)
  fi
fi
if [ -z "${CONTROLS_RUNTIME_CONFIG:-}" ] && [ -n "${PIP_CA:-}" ]; then
  args+=(-pip-ca "$PIP_CA")
fi
if [ -z "${CONTROLS_RUNTIME_CONFIG:-}" ] && [ -n "${PIP_CERT:-}" ]; then
  args+=(-pip-cert "$PIP_CERT")
fi
if [ -z "${CONTROLS_RUNTIME_CONFIG:-}" ] && [ -n "${PIP_KEY:-}" ]; then
  args+=(-pip-key "$PIP_KEY")
fi
if [ -z "${CONTROLS_RUNTIME_CONFIG:-}" ] && [ -n "${PIP_SERVER_NAME:-}" ]; then
  args+=(-pip-server-name "$PIP_SERVER_NAME")
fi
if [ -z "${CONTROLS_RUNTIME_CONFIG:-}" ] && [ -n "${OPA_URL:-}" ]; then
  args+=(-opa-url "$OPA_URL")
fi
if [ -z "${CONTROLS_RUNTIME_CONFIG:-}" ] && [ -n "${OPA_TIMEOUT:-}" ]; then
  args+=(-opa-timeout "$OPA_TIMEOUT")
fi
/usr/local/bin/extproc "${args[@]}" &
ext=$!
trap 'kill "$ext" 2>/dev/null || true' TERM INT
envoy -c /etc/controls/envoy.yaml \
  --service-cluster "${SERVICE_NAME:-sample}" \
  --service-node "$(hostname)" &
env=$!
wait -n "$ext" "$env"
