# Changelog

All notable changes to `controls-core`. Independent versions are tracked for the
sidecar, PIP and contracts; see `docs/COMPATIBILITY.md`.

## [0.2.0] — 2026-11-07

### Added

- Envoy `ext_proc` adapter (`sidecar/extproc`) with BUFFERED body handling,
  immediate responses, response masking, body-size limits and fail-closed
  behaviour; gRPC server at `sidecar/cmd/extproc`.
- Reference Envoy configuration: standalone listener and Istio guidance under
  `sidecar/envoy/`.
- Registry ↔ OpenAPI coverage gate (`internal/openapidiff`,
  `tools/openapi-diff`) with tests.
- Benchmarks for decision, PIP-warm-read and 4 KB masking paths; `make bench`.

### Changed

- Go directive raised to 1.26; added Envoy proto and gRPC dependencies.
- `make fmt`/`fmt-check` now operate on tracked Go files only (the previous
  recursive `gofmt` descended into the module cache).

## [0.1.0] — 2026-11-06

### Added

- Reference engine: registry matching, route-bound resource identity,
  self-description cross-check, policy evaluation, DQ evaluation, response
  masking.
- Contract set: endpoint registry schema + spec, mask policy schema + spec,
  decision log schema, PIP OpenAPI.
- PIP reference implementation over an in-memory read-only repository, with
  batch attribute resolution and classification metadata.
- PIP client with field projection, TTL cache, request coalescing, circuit
  breaker and fail-closed behaviour.
- Envoy-independent HTTP harness (`sidecar/cmd/sidecar`) for local development
  and integration testing.
- Integration suite covering allow/deny, masking, self-description mismatch,
  rejected fields, blocking DQ, unregistered routes and PIP outage.
- Documentation: architecture, security threat model, testing strategy,
  runbook, governance, compatibility matrix, ADR-0001–0009.

### Known limitations

- Envoy `ext_proc` adapter is not yet implemented.
- `freshness` is validated but not enforced.
- Classification-driven mask-all fallback is not wired.
- Reference rule engine, not OPA/Cedar.
- Decision log goes to stdout only.
