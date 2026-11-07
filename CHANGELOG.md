# Changelog

All notable changes to `controls-core`. Independent versions are tracked for the
sidecar, PIP and contracts; see `docs/COMPATIBILITY.md`.

## [0.31.1] — 2026-11-07

### Changed

- `examples/mtls-caller` now resolves attributes through an HTTP source and
  restricts it with `allowedCallers: ["sidecar"]`, exercising caller
  authorization in addition to mutual TLS.

## [0.31.0] — 2026-11-07

### Added

- `examples/pip-outage`: fail-closed sample proving a PIP outage returns 503
  and never calls the upstream.
- `examples/mtls-caller`: sidecar → PIP mutual TLS sample with generated
  test certificates.
- Sidecar runtime entrypoint accepts `PIP_CA`/`PIP_CERT`/`PIP_KEY`/
  `PIP_SERVER_NAME`.

### Fixed

- PIP caller identity was extracted after the `require-caller` check, so
  authenticated mTLS requests were rejected with `caller_required`. Identity
  extraction now runs first.

## [0.30.0] — 2026-11-07

### Added

- `examples/payments-write`: self-contained write sample covering owner
  authorization, blocking DQ (required/positive/currency) and rejected PAN
  fields, with contract tests asserting the upstream is never reached.

## [0.29.0] — 2026-11-07

### Added

- `examples/accounts-read`: self-contained sample (app, controls bundle, Envoy
  config, compose, contract test) and a `controls-sidecar` runtime image that
  packages Envoy + extproc as one artifact. `make examples` runs it in CI.
- `make demo`: keeps the Envoy + smokeapp + PIP + extproc stack running for
  interactive testing.
- Local test API (`make api`): `POST /v1/process` evaluates a request and masks
  the supplied upstream response, `POST /v1/test` runs a conformance fixture,
  and `GET /v1/endpoints`, `GET /v1/info`, `/docs` and `/openapi.yaml` make the
  interface discoverable. `-fixtures` serves the PIP in-process; `make api-rules`
  serves the delivery-owned bundle. Contract: `contracts/test-api.yaml`.
- `conformance.Evaluate` exposes the per-fixture decision, masked response and
  checks; `conformance.Run` uses it, so the CLI and the API assert identically.

### Changed

- The local sidecar harness binds to `127.0.0.1:8081` by default and logs a
  warning when bound elsewhere.

## [0.28.2] — 2026-11-07

### Added

- `make testdrive`: non-interactive local bootstrap that starts the test API,
  evaluates a masked account read, asserts redaction, and exits. No Docker.

## [0.28.1] — 2026-11-07

### Changed

- Removed tautological upstream-status assertions from engine BDD scenarios;
  `make smoke` already asserts real 200/201/403 statuses through Envoy. Engine
  scenarios keep decision statuses (403/503).

## [0.28.0] — 2026-11-07

### Added

- Reload tests: rebuilding a runtime keeps the process-lifetime PIP client
  (cache/breaker) and rejects an invalid bundle directory.

## [0.27.0] — 2026-11-07

### Added

- Mask policies may carry an inline `classification` map. It is signed with the
  bundle and takes precedence over PIP classification metadata for fallback
  masking.

## [0.26.0] — 2026-11-07

### Added

- `-audit-out` writes decisions to a dedicated file (0600, append) instead of
  sharing stdout with application logs; the sidecar library exposes
  `extproc.auditOut`.

## [0.25.0] — 2026-11-07

### Security

- Fresh and deny-on-stale endpoints now require a source-reported lag
  (`lagKnown`); missing lag fails closed with `lag_unknown` instead of being
  treated as zero.

### Added

- OPA decisions receive the request context/deadline and bound the response
  body; Cedar receives method/path context and surfaces evaluation errors as
  `cedar:error`.

## [0.24.0] — 2026-11-07

### Security

- Policy comparisons are type-strict: strings are no longer parsed as
  numbers, and string/bool/null are not coerced via `fmt.Sprint`. A
  body-sourced `"true"` or `"5000"` no longer satisfies a typed rule.

### Fixed

- Masking decodes response bodies with `json.Number`, preserving large
  integers and decimal formatting through the mask/re-encode path.

## [0.23.0] — 2026-11-07

### Added

- `deploy/helm/controls-sidecar`: library chart that owns the Envoy + ext_proc
  container/volume injection, with a renderable example under
  `deploy/examples/sidecar-service`.
- `VERSION` as the single version source; the repository steward fails if docs,
  chart tags or changelog disagree with it.

## [0.22.0] — 2026-11-07

### Added

- PIP `/readyz` probes each source's optional `healthUrl` (2s timeout, 2xx
  required) and returns 503 when an attribute source is unavailable.

## [0.21.0] — 2026-11-07

### Security

- Bundle signatures now cover the complete `manifest.json` (environment,
  service, bundle/contract versions), not only the four-file digest.
- PIP per-caller token-bucket rate limiting (`-rate-limit`, `-rate-burst`),
  returning 429 with `Retry-After` and `pip_rejected_total{reason="rate"}`.

## [0.20.0] — 2026-11-07

### Added

- Contract major is validated at bundle load; unknown majors fail closed.
- Audit writes use a bounded async queue (4096) with
  `controls_audit_dropped_total` / `controls_audit_write_failures_total`;
  `-audit-buffer 0` keeps synchronous mode for tests.
- Extproc `/readyz` fails when the policy engine failed to initialise.

## [0.19.0] — 2026-11-07

### Security

- Gateway identity envelope: when `CONTROLS_IDENTITY_HMAC_KEY` is set, the
  sidecar requires a signed, time-bounded `x-controls-identity` header and
  ignores raw `x-subject`/`x-claim-*`.
- PIP `-require-mtls` and `-require-caller` reject unauthenticated API calls;
  classification metadata is caller-authorized too.
- Bundles now require `-bundle-pubkey`; `-allow-unsigned` is an explicit
  development escape.

### Fixed

- `rejectFields` supports `[*]` array wildcards, so array PAN/CVV is rejected.

## [0.18.0] — 2026-11-07

### Fixed

- PIP circuit breaker now half-opens with one probe and no longer extends its
  window on rejections.
- Attribute cache is capped (100k) and no longer caches not-found results.
- Query strings are stripped before route matching; path IDs are decoded and
  traversal characters rejected; request bodies decode with `json.Number`.
- Policy-initialisation failures now flow through the decision hook.
- The engine is pinned for the whole ext_proc stream, so decision and masking
  cannot use different bundle versions.
- DQ patterns compile at load; unknown operators/types and wildcard paths fail
  validation; DQ operator tests added.

## [0.17.0] — 2026-11-07

### Security

- `maskErrors` now defaults to true; a missing mask policy fails closed.
- Fixed `fieldCallers` bypass when the request omits `fields`.
- Empty classification metadata fails closed.
- Mask-policy overlays merge fields, so a locked field cannot be deleted.
- `defaultUnregistered: allow` is rejected at load.
- PIP-advertised TTLs are clamped (`-max-ttl`, default 5m).
- Reload keeps the process-lifetime PIP cache/circuit breaker.
- Duplicate method+path routes are rejected.

### Added

- PIP PDB (`minAvailable: 1`) and preferred pod anti-affinity.
- CI path filters for chart/features/hack/deploy/tools and a `-race` step.
- [docs/ASSESSMENT-2026-11-07.md](docs/ASSESSMENT-2026-11-07.md).

## [0.16.1] — 2026-11-07

### Added

- Load-test parameters in the smoke gate (`LOADTEST_RPS`, `LOADTEST_DURATION`,
  `LOADTEST_P99`, `LOADTEST_CONCURRENCY`); 1,200 RPS reference run recorded in
  TESTING.
- [docs/PILOT-CHECKLIST.md](docs/PILOT-CHECKLIST.md): cluster prerequisites,
  install order, verification, rollback and open risks.

## [0.16.0] — 2026-11-07

### Added

- Per-caller and per-field authorization for HTTP attribute sources:
  `allowedCallers` restricts a source to client-certificate identities
  (SPIFFE URI SAN, DNS SAN or CN); `fieldCallers` restricts individual fields.
  Forbidden requests return 403 and increment the error metric. Without mTLS,
  a non-empty allowlist fails closed.

## [0.15.0] — 2026-11-07

### Added

- Application-level TLS/mTLS for the PIP (`-tls-cert`, `-tls-key`,
  `-client-ca`) and sidecar client TLS (`-pip-ca`, `-pip-cert`, `-pip-key`,
  `-pip-server-name`, `CONTROLS_PIP_*`), with a real mTLS integration test.
- Chart support for `pip.tls` and documented `sidecar.pipTLS` material.

## [0.14.0] — 2026-11-07

### Added

- PIP concurrency limit (`-max-concurrent`, chart default 256) returning
  `503 overloaded` and exposing `pip_rejected_total{reason="concurrency"}`.

## [0.13.1] — 2026-11-07

### Fixed

- PIP attribute responses now carry the version reported by the source instead
  of the hardcoded `fixture-v1`; the HTTP adapter reads `versionHeader`
  (default `X-Resource-Version`, ETag quotes stripped) and unknown versions are
  reported as `unknown`.

## [0.13.0] — 2026-11-07

### Added

- PIP Prometheus metrics on `-metrics-addr` (default `:9090`): requests,
  errors, latency histogram, attribute outcomes and replica lag.
- Chart exposes the PIP metrics port and restricts it to the monitoring
  namespace.

## [0.12.1] — 2026-11-07

### Added

- HTTP attribute sources for the PIP (`-sources`): per-resource URL templates,
  field allowlists, `${ENV}` headers, replica-lag headers and classification
  metadata, with chart ConfigMap/volume/egress support.

### Security

- Attribute-source mTLS: `caFile`/`certFile`/`keyFile`/`serverName`,
  HTTPS-only, redirects refused, no environment proxy.
- Upstream responses capped at 1 MiB; `{id}` cannot be interpolated into the
  URL host.

## [0.11.0] — 2026-11-07

### Added

- `hack/loadtest`: fixed-RPS load generator with p50/p95/p99 and status
  reporting; `make loadtest` and an optional `CONTROLS_LOADTEST=1 make smoke`
  gate (p99 <= 100 ms through Envoy).

## [0.10.1] — 2026-11-07

### Added

- Cluster-controls Jenkins job (`ci/Jenkinsfile.cluster-controls`): renders the
  chart with `helm template` and applies it with `kubectl`; no Helm release
  state, archived manifest for rollback.
- `make cluster-controls`; explicit namespaces on every chart resource.

## [0.10.0] — 2026-11-07

### Added

- External OPA decision engine: `rules.engine: "opa"` plus a decision path;
  base URL from `-opa-url`/`CONTROLS_OPA_URL`; fail-closed, no cache.

## [0.9.1] — 2026-11-07

### Security

- Masked endpoints fail closed on non-JSON response bodies; `allowNonJSON: true`
  is required to opt out (audit M4).

## [0.9.0] — 2026-11-07

### Added

- Bundle reload by polling (`-reload-interval`, 30s) and `SIGHUP`, with
  fingerprinting, signature re-verification, atomic engine swap and
  last-known-good on failure.
- Adapter engine provider; metric family reset for bundle info.

## [0.8.0] — 2026-11-07

### Added

- Optional Cedar policy engine behind `policy.Evaluator`: `policies/*.cedar`
  rendered to `policies.cedar`, entity mapping, deny-by-default and fail-closed
  initialisation.
- Renderer rejects mixing JSON rules and Cedar; digest/signature cover the
  policy file.

## [0.7.0] — 2026-11-07

### Added

- Helm chart (`deploy/helm/controls-plane`) for the PIP with NetworkPolicies
  (gateway → sidecar, sidecar → PIP, monitoring → metrics) and optional Istio
  `PeerAuthentication: STRICT` / PIP `AuthorizationPolicy`.
- Reference PIP and extproc images (non-root); chart CI job.

## [0.6.0] — 2026-11-07

### Security

- Ed25519 bundle signing (`bundle-sign`, `bundle-keygen`) and sidecar
  verification (`-bundle-pubkey`); the digest is recomputed from the files.
- Tools image runs as UID 10001; Jenkins agent no longer root; smoke Envoy
  pinned by digest.
- `make vulncheck` / CI `govulncheck` step.
- Bundle overlays reject `defaultUnregistered: allow`; strict overlay decoding.
- PIP body (64 KiB) and batch (100) limits.
- ext_proc requires `CONTROLS_AUDIT_SALT`.
- Audit report at [docs/SECURITY-AUDIT.md](docs/SECURITY-AUDIT.md).

## [0.5.0] — 2026-11-07

### Added

- Classification-driven mask fallback: `fallback: "classification"` masks
  fields tagged `pii.*` not explicitly covered; explicit rules win and missing
  classification fails closed.
- Classification metadata cached in the PIP client (default one hour).

## [0.4.0] — 2026-11-07

### Added

- Decision audit (`internal/audit`): hashed identifiers, 100% deny logging,
  sampled allows, bundle/policy versions.
- Prometheus metrics (`internal/metrics`) on a separate admin listener:
  decisions, latency, PIP cache, replica lag, stale denials, DQ and bundle info.

## [0.3.0] — 2026-11-07

### Added

- Deterministic bundle renderer (`tools/bundle`) with environment/service
  overlays, validation and a digest-bearing manifest.
- Conformance runner (`tools/conformance`) executing fixtures against a
  rendered bundle.
- `service` ownership on registry endpoints; Jenkins pipeline contract and
  tools image.

## [0.2.0] — 2026-11-07

### Added

- Envoy `ext_proc` adapter with BUFFERED bodies, immediate responses, masking,
  body limits and fail-closed behaviour; reference Envoy/Istio configuration.
- Registry ↔ OpenAPI gate; engine benchmarks.
- Live Envoy smoke test, BDD acceptance suite (`features/`, `make acceptance`).
- Replica-lag reporting and `freshness`/`maxStalenessSeconds` enforcement.
- `-debug`, `-metrics-addr`, `-audit-sample`, `-audit-salt` flags.

### Fixed

- Pseudo-headers read from `raw_value`; bodyless requests decide on headers
  `end_of_stream`; mutated bodies update `content-length` (raw) and require
  `allow_content_length_header`.

## [0.1.0] — 2026-11-06

### Added

- Reference engine: registry matching, route-bound identity, self-description
  cross-check, policy, DQ, masking.
- Contracts: registry, mask policy, decision log and PIP OpenAPI.
- PIP over an in-memory repository; PIP client with cache, coalescing and
  breaker.
- HTTP harness, integration suite and core documentation (architecture,
  security, testing, runbook, governance, compatibility, ADR-0001–0009).
