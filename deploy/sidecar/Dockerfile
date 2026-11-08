# Sidecar runtime image: Envoy + extproc in one artifact for samples.
# Build `make build` first so bin/extproc exists.
FROM envoyproxy/envoy:v1.40.0@sha256:459f2a6404c340fe6afccd69d85527012d47f3c036765493e6f8f3113fa384de
ARG EXTPROC_BIN=bin/extproc
COPY ${EXTPROC_BIN} /usr/local/bin/extproc
COPY deploy/sidecar/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
