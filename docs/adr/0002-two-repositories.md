# ADR-0002 — Two repositories: core monorepo plus rules repo

Status: accepted (2026-11-06)

## Context

Core engineering builds the engine, PIP and contracts. The delivery team owns
rules and runtime configuration. Sidecar, PIP and contracts share an owner and
change together; splitting them would introduce version-skew between artifacts
that are developed atomically.

## Decision

- `controls-core`: monorepo containing sidecar, PIP, contracts and docs.
  Produces three independently versioned artifacts (`sidecar/v…`, `pip/v…`,
  `contracts/v…`).
- `controls-rules`: delivery-owned rules, mask policies, registry entries and
  environment overlays.

Contracts are published as a versioned package so the rules repo and service
teams consume them without cloning core.

## Consequences

- One integration suite covers sidecar ↔ PIP ↔ fixtures.
- Path-filtered CI keeps the monorepo build fast.
- Splitting PIP or contracts later is routine; merging drifted repos is not.

## Alternatives

- **Four repos**: more ceremony, no ownership benefit at this team size.
- **Single repo for everything**: weakens the delivery boundary and permissions.
