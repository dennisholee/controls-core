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
| `CONTROLS_RUNTIME_CONFIG` | optional path to `sidecar.yaml`; replaces the legacy flag set |
| `CONTROLS_BUNDLE_PUBKEY` | bundle public key path (legacy flag mode also accepts `-bundle-pubkey`) |
| `PIP_URL` | PIP base URL (legacy flags; required unless `CONTROLS_RUNTIME_CONFIG` is set) |
| `PIP_API_VERSION` | expected PIP contract major.minor (default `1.0`; empty disables) |
| `CONTROLS_AUDIT_SALT` | audit hash salt (legacy flags; config uses `env:` refs) |
| `SERVICE_NAME` | Envoy service cluster (optional) |
| `PIP_CA` / `PIP_CERT` / `PIP_KEY` | PIP TLS / client certificate files |
| `PIP_SERVER_NAME` | expected PIP server name for TLS |
| `OPA_URL` | OPA base URL when legacy flags are used (`policy.opa.url` in `sidecar.yaml`) |
| `OPA_TIMEOUT` | OPA decision timeout (default 100ms) |

The example config is [`sidecar.example.yaml`](sidecar.example.yaml).
Precedence is CLI flags > env > file > defaults. The entrypoint runs
`-allow-unsigned` and an in-process audit queue for sample use when no runtime
config is supplied. Production should use the Helm library chart (two containers, signed
bundle, `-bundle-pubkey`) instead of this combined image.

## Example

`examples/accounts-read` uses this image with a compose stack; see its
`compose.yaml` file.
