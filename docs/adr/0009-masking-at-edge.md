# ADR-0009 — Masking at the edge; application logs out of scope

Status: accepted (2026-11-06)

## Context

The sidecar masks responses after the application has produced them. Unmasked
PII therefore exists in application memory and may be written to application
logs. A strict "PII never leaves the data layer" requirement would force
in-application field filtering.

The scope here is subject journeys, where configurable masking is the control
and full data minimisation is not mandated.

## Decision

Accept edge masking as the enforcement point. Do not require in-application
masking in v1.

## Consequences

- No application changes; central management is preserved.
- Application logs and heap may contain unmasked data; log hygiene is the
  application's responsibility.
- If a future journey becomes subject to strict data-minimisation rules, an
  in-application field filter will be needed in addition to the sidecar. The
  mask contract already returns a permitted-field set, so the in-app
  implementation can reuse the same policies.

## Alternatives

- In-app masking: rejected for v1 (intrusion, rebuild coupling), retained as a
  future option for regulated journeys.
- No masking: rejected.
