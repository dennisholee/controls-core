# ADR-0008 — Layered configuration, materialised by the pipeline

Status: accepted (2026-11-06)

## Context

Configuration exists at global, environment, service and (rarely) instance
levels, owned by different teams. Runtime deep-merging is expensive,
non-deterministic under partial reload, and hard to reason about during
incidents.

## Decision

- Resolve global → environment → service → instance at **pipeline** time and
  emit one immutable, signed effective bundle per service.
- The sidecar loads a complete bundle, validates it, and swaps it atomically.
- Structural/engine settings are core-owned and deploy-time only; rules and
  mask policies are delivery-owned and hot-reloadable.
- Unknown keys are rejected; `null` means inherit, `[]` means clear.

## Consequences

- "What will prod do?" is answered by inspecting an artifact, not by merging
  configs mentally.
- Rollback is redeploying the previous bundle digest.
- The pipeline must implement layering and validation correctly; that code is
  as important as the sidecar.

## Alternatives

- Runtime merge: rejected (CPU, determinism, debuggability).
- One global file: rejected (blast radius, ownership ambiguity).
