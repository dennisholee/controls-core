# Sidecar runtime image

`deploy/sidecar/Dockerfile` builds a single sample/runtime artifact containing
Envoy and the ext_proc binary. Samples consume this image instead of building
the sidecar from source.

```bash
make build   # produces bin/extproc
docker build --build-arg EXTPROC_BIN=bin/extproc \
  -f deploy/sidecar/Dockerfile -t controls-sidecar:dev .
```

For a published image, build the binary for the target platform, pin the Envoy
base by digest, and publish the result by digest.

## Runtime contract

| Path / env | Purpose |
|---|---|
| `/etc/controls/bundle` | rendered bundle (registry, rules, masks, DQ) |
| `/etc/controls/envoy.yaml` | Envoy config; app cluster + ext_proc on `127.0.0.1:50051` |
| `PIP_URL` | PIP base URL (required) |
| `CONTROLS_AUDIT_SALT` | audit hash salt |
| `SERVICE_NAME` | Envoy service cluster (optional) |
| `PIP_CA` / `PIP_CERT` / `PIP_KEY` | PIP TLS / client certificate files |
| `PIP_SERVER_NAME` | expected PIP server name for TLS |
| `OPA_URL` | OPA base URL when the bundle uses `engine: opa` |
| `OPA_TIMEOUT` | OPA decision timeout (default 100ms) |

The entrypoint runs `-allow-unsigned` and an in-process audit queue for sample
use. Production should use the Helm library chart (two containers, signed
bundle, `-bundle-pubkey`) instead of this combined image.

## Example

`examples/accounts-read` and `payments-service-demo` use this image with a
compose stack; see their `compose.yaml` files.
