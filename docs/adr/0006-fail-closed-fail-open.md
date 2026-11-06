# ADR-0006 — Entitlements fail closed; DQ fails open

Status: accepted (2026-11-06)

## Context

Entitlements and data quality have different failure semantics. An
authorization failure that defaults to allow is a security incident; a data
quality check that blocks a valid journey on a false positive is an outage.

## Decision

- Entitlement: PIP unavailable, missing attributes, invalid config or unmatched
  route ⇒ **deny**.
- DQ: violations are recorded and only `blocking: true` rejects. Default is
  observe-only, with quarantine and alerting preferred over rejection.
- Masking: if masking cannot be applied, deny rather than return unmasked.

## Consequences

- PIP availability is on the critical path for PIP-dependent journeys; the
  cache and breaker are required, not optional.
- DQ can be rolled out in shadow mode and promoted rule by rule.
- Operators cannot "fix" an entitlement outage by disabling controls; the
  runbook mandates gateway route blocking instead.

## Alternatives

- Fail-open entitlements: rejected.
- Fail-closed DQ: rejected; false positives would block customer journeys.
