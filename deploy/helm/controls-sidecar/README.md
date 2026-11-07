# controls-sidecar (library chart)

Injects the Envoy and ext_proc containers into a service Deployment. The chart
is `type: library`; it renders nothing on its own.

## Usage

```yaml
containers:
  - name: app
    image: ...
  {{- include "controls-sidecar.containers" . | nindent 2 }}
volumes:
  {{- include "controls-sidecar.volumes" . | nindent 2 }}
```

See [`../../examples/sidecar-service`](../../examples/sidecar-service) for a
renderable parent.

## Values

| Key | Purpose |
|---|---|
| `envoy.image` / `extproc.image` | container images (pin by digest in production) |
| `envoy.port` / `envoy.adminPort` | Envoy listener/admin ports |
| `extproc.port` / `extproc.metricsPort` | ext_proc gRPC / metrics ports |
| `extproc.pipURL` | PIP base URL |
| `extproc.pipCA` / `pipCert` / `pipKey` / `pipServerName` | PIP mTLS files and SNI |
| `extproc.auditSample` / `auditBuffer` / `auditOut` | audit sampling, queue and optional file |
| `extproc.allowUnsigned` | development only; production requires a bundle pubkey |
| `volumes.envoyConfig` | ConfigMap containing `envoy.yaml` |
| `volumes.bundle` | signed bundle ConfigMap |
| `volumes.secrets` | Secret with audit salt, identity key and bundle public key |
| `secrets.auditSaltKey` / `identityKey` / `bundlePubKey` | keys inside that Secret |

## Parent responsibilities

- Provide the Envoy ConfigMap (app cluster + ext_proc `127.0.0.1:50051`).
- Provide the rendered/signed bundle.
- Provide the Secret: `audit-salt`, `identity-hmac`, `public-key.pem`, and any
  PIP client certificate files.
- Mount the PIP client material at the paths named in `extproc.pip*`.
- Keep `allowUnsigned: false` outside development.

## Security

- The sidecar does not terminate user authentication; the gateway verifies and
  injects identity.
- The bundle public key is mounted read-only; the signing key never enters the
  pod.
- Metrics ports should be admitted only from the monitoring namespace.
