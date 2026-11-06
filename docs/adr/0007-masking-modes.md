# ADR-0007 — Masking modes and locked fields

Status: accepted (2026-11-06)

## Context

Field-level entitlement needs configurable masking per journey, but some fields
must never be configurable to "clear".

## Decision

- Modes: `none`, `full`, `partial`.
- `partial` requires explicit parameters (`keepPrefix`, `keepSuffix`,
  `preserveDomain`, `keep: year`); bare `partial` is invalid.
- Locked fields `ssn`, `pan`, `cvv`, `cardNumber` are fixed to `full` and
  validated at bundle load and in the delivery pipeline.
- Write requests reject configured fields (`rejectFields`) rather than masking
  them.
- Error responses are masked by default (`maskErrors: true`).

## Consequences

- Masked output is part of the contract; changing partial semantics is a major
  version change.
- Downstream consumers can rely on stable masked shapes.
- Validation prevents the most common compliance failure (a journey quietly
  relaxing a sensitive field).

## Alternatives

- Boolean mask flag: too coarse; partial masking is a real requirement.
- Per-field free-form regex replacements: hard to review and test.
