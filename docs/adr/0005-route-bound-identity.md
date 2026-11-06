# ADR-0005 — Route-bound identity; payload is never authoritative

Status: accepted (2026-11-06)

## Context

Self-describing payloads simplify generic extraction, but if the sidecar trusts
payload `_type`/`_id`, a caller can choose which policy and which resource
applies to them — an entitlement bypass.

## Decision

- Resource type and resource identity are bound to the route in the registry.
- Payload `_type`/`_id` are optional cross-checks when `selfDescribing: true`;
  any mismatch returns 403 and increments a metric.
- Unregistered routes are denied in production (`defaultUnregistered: deny`).
- The registry is diffed against the service OpenAPI document in CI.

## Consequences

- A new endpoint cannot ship without a registry entry.
- Generic extraction still works; only the authority is server-side.
- Mismatch metrics provide an attempted-bypass signal.

## Alternatives

- Trust payload type: rejected, trivially spoofable.
- Require payload type only: still caller-controlled.
