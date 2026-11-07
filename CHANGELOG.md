# Changelog

All notable changes to `controls-core`. Independent versions are tracked for the
sidecar, PIP and contracts; see `docs/COMPATIBILITY.md`.

## [0.5.0] — 2026-11-07

### Security

- Ed25519 bundle signing (`bundle-sign`, `bundle-keygen`) and sidecar
  verification via `-bundle-pubkey`; the digest is recomputed from the four
  files, so any modification fails startup.
- Bundle overlays reject `defaultUnregistered: allow`; overlay documents are
  decoded strictly, so unknown keys fail the render.
- PIP enforces a 64 KiB body limit and a 100-resource batch cap.
- ext_proc refuses to start without `CONTROLS_AUDIT_SALT`.
- Full audit report at [docs/SECURITY-AUDIT.md](docs/SECURITY-AUDIT.md).

### Added

- Classification-driven mask fallback: `fallback: "classification"` masks
  fields tagged `pii.*` that the policy does not explicitly cover. Explicit
  rules win; missing classification fails closed.
- PIP classification metadata is cached (PIP-advertised TTL, default one hour)
  and exposed for the fail-safe path.

## [0.4.0] — 2026-11-07

### Added

- Decision audit (`internal/audit`) emitting `decision-log.schema.json`-shaped
  JSON with salted SHA-256 subject/resource hashes, 100% deny logging and
  configurable allow sampling.
- Prometheus metrics (`internal/metrics`) on a separate admin listener:
  decisions, latency histogram, PIP cache outcomes, replica lag, stale denials,
  DQ violations and bundle info.
- Engine decision hooks carrying request id, cache state and latency.
- Ext_proc flags `-metrics-addr`, `-audit-sample`, `-audit-salt`; bundle
  manifest metadata is loaded and reported.
- Smoke test now asserts audit output and metrics.

## [0.3.0] — 2026-11-07

### Added

- Deterministic bundle renderer (`internal/bundle`, `tools/bundle`): merges
  policies, mask policies, registry and DQ rules; applies environment/service
  overlays with typo protection; validates through `config.LoadDir`; emits a
  `manifest.json` with a SHA-256 digest and counts.
- Conformance runner (`internal/conformance`, `tools/conformance`): executes
  fixture files against a rendered bundle, per service, asserting decisions,
  reasons and masked fields.
- `service` field on registry endpoints and per-service bundle filtering.
- Jenkins integration contract ([docs/PIPELINE.md](docs/PIPELINE.md)), reference
  [Jenkinsfile](ci/Jenkinsfile) and tools image
  ([deploy/Dockerfile.tools](deploy/Dockerfile.tools)).

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
- Live Envoy smoke test (`make smoke`) using Envoy 1.40 in Docker: allow+mask,
  deny, self-description mismatch, authorized write and unregistered route.
- `-debug` flag on the ext_proc server for request-header visibility.
- BDD acceptance suite (`features/*.feature`) executed against the in-process
  engine and, through `make smoke`, the live Envoy listener; `make acceptance`
  target and the conformance-steward role doc.
- Replica-lag reporting from the PIP (`lagSeconds`, `-lag-seconds` fixture flag)
  and enforcement of `freshness`/`maxStalenessSeconds` in the engine: `fresh`
  bypasses the sidecar cache, both fresh and `deny-on-stale` return
  `503 stale_attributes` above the bound.

### Changed

- Go directive raised to 1.26; added Envoy proto and gRPC dependencies.
- `make fmt`/`fmt-check` now operate on tracked Go files only (the previous
  recursive `gofmt` descended into the module cache).

### Fixed

- Read pseudo-headers from `raw_value`; Envoy sends `:method`, `:path` and
  `:status` only as raw bytes.
- Evaluate bodyless requests on the request-headers `end_of_stream` message;
  Envoy does not send an empty request body for GET.
- Update `content-length` (as `raw_value`) when mutating a response body and
  enable `allow_content_length_header`, preventing Envoy's
  `mismatch_between_content_length_and_the_length_of_the_mutated_body` 500.

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
