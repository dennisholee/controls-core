# ADR-0010 — Optional Cedar policy engine

Status: accepted (2026-11-07)

## Context

The reference structured-rule engine is intentionally small: it covers
attribute comparisons, deny-first semantics and resource attributes, but it is
not a general policy language. Cedar offers a typed, analyzable language with
entity attributes and set/relationship expressions, and has a pure-Go
implementation suitable for the sidecar.

## Decision

- The bundle selects its engine with `rules.engine`: `rules` (default) or
  `cedar`.
- Cedar policies live in `policies/*.cedar` and are rendered into
  `policies.cedar`. JSON rules and Cedar policies cannot be mixed in one
  bundle.
- Mapping: principal `User::"<sub>"` with claims as attributes; resource
  `<ResourceType>::"<id>"` with the resolved attribute map; action
  `Action::"<action>"`.
- Cedar denies by default. A parse or evaluation failure fails closed.
- The `policy.Evaluator` interface remains the seam; OPA can be added later
  without changing the engine.

## Consequences

- Delivery teams can choose expressiveness per service without changing the
  sidecar.
- Cedar evaluation is heavier than the reference evaluator (measured ~1.2 µs
  and 2.9 KB per decision on an M2, versus sub-microsecond for simple rules);
  benchmark before enabling it on a high-RPS service.
- No Cedar schema validation is performed yet; entity shapes are conventional.
- Cedar relationships (parents) are not populated; ReBAC-style policies need a
  future entity provider.

## Alternatives

- OPA/Rego: heavier runtime and data model; deferred.
- Extending the JSON rule DSL indefinitely: poor tooling and no formal
  analysis.
