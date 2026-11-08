# Repository inventory

Status as of 2026-11-08. This is the canonical map for the platform.

| Repository | Status | Purpose |
|---|---|---|
| [`controls-core`](https://github.com/dennisholee/controls-core) | Canonical | Sidecar engine, PIP, contracts, tools, Helm charts, docs |
| [`controls-rules`](https://github.com/dennisholee/controls-rules) | Canonical | Policies, masks, DQ, registry entries, environment overlays |
| [`party-mastering-service`](https://github.com/dennisholee/party-mastering-service) | Canonical demo | OpenAPI 3.1 Party service, generated Java, four controlled endpoints |
| `examples/` (this repo) | Minimal examples | Focused executable samples; not full services |

## Local artifacts

`target/`, `.m2-repo/`, `.gocache/`, `.gopath/`, `.tmp/`, `bin/` and generated
Helm packages are ignored and must not be committed. Remove large caches before
running code-map scans.

## History

The archived `payments-service-demo` was deleted on 2026-11-08. Its external-OPA
policy is at [`docs/archive/payments-authz.rego`](docs/archive/payments-authz.rego).
