# Sidecar runtime image: Envoy + extproc in one artifact for samples.
# Build `make build` first so bin/extproc exists.
FROM envoyproxy/envoy:v1.40.0
ARG EXTPROC_BIN=bin/extproc
COPY ${EXTPROC_BIN} /usr/local/bin/extproc
COPY deploy/sidecar/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
