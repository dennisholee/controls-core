#!/usr/bin/env bash
# Test-only certificates. Server SAN is the compose service name `pip`.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
if [ -f ca.crt ] && [ -f server.crt ] && [ -f client.crt ]; then exit 0; fi
openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt \
  -subj "/CN=controls-sample-ca" -days 2 >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -keyout server.key -out server.csr \
  -subj "/CN=pip" >/dev/null 2>&1
printf 'subjectAltName=DNS:pip\n' > ext.cnf
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 2 -extfile ext.cnf >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -keyout client.key -out client.csr \
  -subj "/CN=sidecar" >/dev/null 2>&1
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days 2 >/dev/null 2>&1
rm -f server.csr client.csr ext.cnf
