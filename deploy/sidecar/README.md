# Sidecar runtime image

`deploy/sidecar/Dockerfile` builds a single sample/runtime artifact containing
Envoy and the ext_proc binary. Samples consume this image instead of building
the sidecar from source.

```bash
make build   # produces bin/extproc
docker build --build-arg EXTPROC_BIN=bin/extproc \
  -f deploy/sidecar/Dockerfile -t controls-sidecar:dev .
```

The entrypoint expects:

| Path / env | Purpose |
|---|---|
| `/etc/controls/bundle` | rendered bundle (registry, rules, masks, DQ) |
| `/etc/controls/envoy.yaml` | Envoy config; app cluster + ext_proc on `127.0.0.1:50051` |
| `PIP_URL` | PIP base URL |
| `CONTROLS_AUDIT_SALT` | audit hash salt |
| `SERVICE_NAME` | Envoy service cluster (optional) |

For production, prefer two containers (Envoy + extproc) managed by the Helm
library chart; this combined image exists so samples and local compose are
self-contained.
