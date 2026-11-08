# Controls Plane — platform overview

This repository is the core of a three-repository controls plane for
data-level entitlement, masking and data-quality enforcement.

| Repository | Purpose |
|---|---|
| [`controls-core`](https://github.com/dennisholee/controls-core) | Sidecar engine, PIP, contracts, tools, Helm charts and reference docs |
| [`controls-rules`](https://github.com/dennisholee/controls-rules) | Delivery-owned policies, masks, DQ rules, registry entries and overlays |
| [`party-mastering-service`](https://github.com/dennisholee/party-mastering-service) | Canonical Spring Boot + OpenAPI 3.1 Party demo |

## Canonical demo

Use `party-mastering-service`. It covers OpenAPI generation, entitlement,
masking, DQ, conformance, signed identity, signed bundles and static/dynamic
replica lag.

## Design

```
client → gateway (authn + coarse authz) → Envoy sidecar → Spring Boot service
                                             │ ext_proc
                                             ▼
                                      controls engine
                                             │
                                             ▼
                                      PIP attributes
```

Rules and bundles are centrally managed; the sidecar fails closed for
registered routes. See [`ARCHITECTURE.md`](ARCHITECTURE.md),
[`docs/SIDECAR-GUIDE.md`](docs/SIDECAR-GUIDE.md) and
[`docs/DEPRECATION.md`](docs/DEPRECATION.md).

## Release state

`v0.41.0` is the current local release: Spring Boot 4.1 demo, OpenAPI 3.1,
Go 1.27, Alpine 3.24 and typed Envoy HTTP options. Cluster verification and
external image publication remain environment/CI tasks.
