# Architecture Decision Records

Statuses: proposed / accepted / superseded.

| ADR | Decision |
|---|---|
| [0001](0001-sidecar-over-starter.md) | Enforce through a sidecar, not an in-process starter |
| [0002](0002-two-repositories.md) | Two repositories: core monorepo plus rules repo |
| [0003](0003-ext-proc-buffered.md) | Envoy `ext_proc` with buffered bodies |
| [0004](0004-pip-data-service.md) | PIP is a data service over read replicas |
| [0005](0005-route-bound-identity.md) | Route-bound identity; payload is never authoritative |
| [0006](0006-fail-closed-fail-open.md) | Entitlements fail closed; DQ fails open |
| [0007](0007-masking-modes.md) | Masking modes and locked fields |
| [0008](0008-config-layering.md) | Layered configuration, materialised by the pipeline |
| [0009](0009-masking-at-edge.md) | Masking at the edge; application logs out of scope |
| [0010](0010-cedar-policy-engine.md) | Optional Cedar policy engine |
| [0011](0011-bundle-reload.md) | Bundle reload by polling plus SIGHUP |
| [0012](0012-external-opa-engine.md) | External OPA decision engine |

New decisions follow the same format: Status, Context, Decision, Consequences,
Alternatives. A decision that contradicts an accepted ADR requires a
superseding ADR, not an undocumented exception.
