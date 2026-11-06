# ADR-0001 — Enforce through a sidecar, not an in-process starter

Status: accepted (2026-11-06)

## Context

The service fleet is JVM-only, which would make a Spring Boot starter the
cheapest place to evaluate rules. However, entitlements and data quality are a
separately managed capability: the implementation must be centrally maintained
and upgraded without requiring application teams to rebuild and redeploy.

Traffic is north–south, ~5,000 RPS across at most 5 pods; sidecar resource cost
is therefore negligible.

## Decision

Enforce through an Envoy-based sidecar that is centrally built, versioned and
injected. Applications are not modified.

## Consequences

- Engine and adapter upgrades are rolling pod restarts, not application releases.
- One artifact to maintain across services; consistent behaviour and telemetry.
- The sidecar must solve payload semantics and attribute lookup externally
  (see ADR-0003/0004), and cannot see domain objects directly.
- Debugging spans an extra hop; decision logs must be rich enough to compensate.

## Alternatives

- **Spring Boot starter**: lower latency, but couples control upgrades to
  application releases and violates the central-management requirement.
- **Gateway-only**: cannot see payload semantics or database state.
