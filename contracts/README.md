# Contracts

Normative artifacts shared by the sidecar, the PIP, the delivery team's rules
repo and service teams. Anything in this directory is versioned and published;
prose documents explain the machine-readable files, not the other way around.

| Artifact | Purpose | Versioned independently |
|---|---|---|
| [`registry.schema.json`](registry.schema.json) | Endpoint registry (routes → resource types, identity, masking) | yes |
| [`SPEC-registry.md`](SPEC-registry.md) | Normative explanation of registry semantics and authority rule | with registry |
| [`mask-policy.schema.json`](mask-policy.schema.json) | Mask policy document | yes |
| [`SPEC-mask-ops.md`](SPEC-mask-ops.md) | Normative mask semantics, locked fields, failure behaviour | with mask |
| [`decision-log.schema.json`](decision-log.schema.json) | Decision log line (hashed identifiers, no payloads) | yes |
| [`pip-api.yaml`](pip-api.yaml) | PIP HTTP API | yes |
| [`test-api.yaml`](test-api.yaml) | Local test API: request simulator and conformance fixture runner | no (developer tooling) |

## Versioning

Semantic versions: `MAJOR.MINOR`.

- **MINOR** — additive, backward-compatible fields or enum values.
- **MAJOR** — removed/renamed fields, changed defaults, changed enforcement
  behaviour (for example a field that was advisory becoming blocking).

The sidecar refuses to start when the bundle's declared contract version is
outside its supported range. See [`../docs/COMPATIBILITY.md`](../docs/COMPATIBILITY.md).

## Publication

CI publishes `contracts@<version>` (schemas + specs + OpenAPI) to the internal
artifact repository. The rules repo and service teams consume the published
package; they do not clone this repository to read it.

## Ownership

Core engineering owns this directory. Changes require review from core and, for
`SPEC-mask-ops.md` and locked fields, security sign-off. See
[`../docs/GOVERNANCE.md`](../docs/GOVERNANCE.md).
