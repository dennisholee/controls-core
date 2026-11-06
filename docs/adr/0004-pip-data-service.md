# ADR-0004 — PIP is a data service over read replicas

Status: accepted (2026-11-06)

## Context

Data entitlement needs authoritative resource attributes and field
classification. The sidecar cannot connect to databases directly without
schema coupling, connection-pool multiplication and bypassing service
boundaries.

## Decision

Build a Policy Information Point exposed over HTTP, backed by read replicas
through per-domain adapters with curated, parameterized queries. It returns
attributes and classification only; it does not make decisions.

The sidecar caches responses locally (TTL, projection-aware keys), coalesces
misses, applies a timeout and circuit breaker, and fails closed on error.

## Consequences

- Decisions stay local and fast; PIP is not a latency-critical PDP.
- PIP outage denies PIP-dependent requests; acceptable and explicit.
- Replica lag becomes an entitlement-correctness concern; `freshness` classes
  are part of the registry contract and lag must be measured.
- Field projection is mandatory to minimise data exposure.

## Alternatives

- **Sidecar reads replicas directly**: rejected (schema coupling, credentials).
- **Central PDP**: rejected (hot-path SPOF, difficult to evolve per service).
- **Precomputed claims**: insufficient cardinality.
