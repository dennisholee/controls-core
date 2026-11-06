# Governance

## 1. Ownership

| Artifact | Core engineering | Delivery team | Service teams |
|---|---|---|---|
| Sidecar engine, Envoy chain, images | own | read | read |
| PIP service, adapters, caches | own | read | read |
| Contracts (`contracts/`, schemas, specs) | own | read | read |
| Platform config (failure modes, endpoints, limits, profiles) | own | read | read |
| Authorization rules, DQ rules, mask policies | review | own | consult |
| Registry entries (route → resource mapping) | review | merge + own | author |
| OpenAPI `x-resource-*` annotations | review | read | own |
| Environments / promotion | consult | own | read |

Rule of thumb: **core owns the engine and the contract; delivery owns the data
that drives it; service teams own the API surface facts.**

## 2. Change processes

### Engine or contract change

1. ADR for behaviour changes; schema change with version bump.
2. Core review + security review when masking, identity or locked fields change.
3. Publish `contracts@<version>` and the sidecar/PIP artifacts.
4. Compatibility matrix updated.

### Rule or mask policy change

1. PR to `controls-rules`.
2. Pipeline: schema validation, locked-field check, conformance suite.
3. Delivery review; security review for mask changes that reduce masking.
4. Signed bundle, digest recorded in the deployment.

### Registry entry (new endpoint)

1. Service team annotates OpenAPI and supplies the mapping.
2. Delivery reviews and merges.
3. OpenAPI diff gate must pass; fixture cases added.
4. Deploy bundle; verify endpoint count and digest.

### Locked fields

`ssn`, `pan`, `cvv`, `cardNumber` are locked to `full`. Changing this set is a
contract-level change requiring security sign-off and an ADR.

## 3. Review rules

| Change | Required reviewers |
|---|---|
| Engine, PIP code | core (2) |
| Contract schema | core + security (if masking/identity) |
| Rules affecting entitlements | delivery + security |
| Mask policy relaxation | delivery + security + data owner |
| Locked field change | core + security + data owner |
| Break-glass | core on-call + security |

## 4. Service levels

- New rule primitive: triaged within 5 business days; delivery unblocked with a
  documented workaround if longer.
- Registry review: 2 business days.
- Security review of mask relaxation: 3 business days.
- Production incident: per RUNBOOK.

## 5. Break-glass

Only core on-call holds break-glass authority. Permitted actions are listed in
[`../RUNBOOK.md`](../RUNBOOK.md). Every use is logged, time-boxed and reviewed
within one business day.

## 6. Decision records

Architecture decisions live in [`adr/`](adr/). A change that contradicts an ADR
requires a superseding ADR, not an undocumented exception.
