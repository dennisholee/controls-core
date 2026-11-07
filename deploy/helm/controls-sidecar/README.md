# controls-sidecar (library chart)

Injects the two controls containers into a service Deployment:

```yaml
containers:
  - name: app
    image: ...
  {{- include "controls-sidecar.containers" . | nindent 2 }}
volumes:
  {{- include "controls-sidecar.volumes" . | nindent 2 }}
```

The parent chart supplies:

- `volumes.envoyConfig` — ConfigMap with `envoy.yaml` (see
  `controls-core/sidecar/envoy/static-sidecar.yaml`).
- `volumes.bundle` — the signed bundle ConfigMap.
- `volumes.secrets` — Secret with `audit-salt`, `identity-hmac` and the bundle
  public key (or leave `allowUnsigned` only for development).

`extproc.pipCA/pipCert/pipKey` are file names inside that Secret; the parent
must mount the PIP client material there.

The chart is `type: library`; it renders nothing on its own. See
`deploy/examples/sidecar-service` for a renderable parent.
