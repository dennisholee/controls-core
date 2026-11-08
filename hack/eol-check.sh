#!/usr/bin/env bash
# Fail on known-EOL bases and deprecated Envoy cluster fields.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
fail=0

if grep -RInE '^FROM alpine:3\.(20|21)([[:space:]]|$)' \
    --include='Dockerfile*' . ; then
  echo "eol-check: use a supported Alpine base (3.23/3.24), pinned by digest" >&2
  fail=1
fi

if grep -RInE '^      http2_protocol_options:' --include='*.yaml' . ; then
  echo "eol-check: migrate Cluster.http2_protocol_options to typed HttpProtocolOptions" >&2
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "eol-check OK: no EOL Alpine base or deprecated Envoy cluster field"
