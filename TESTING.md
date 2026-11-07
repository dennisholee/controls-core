# Testing

The controls plane is configuration-driven, so tests must prove both the engine
and **the effective configuration**. Conformance is a pipeline gate, not a
manual review step.

## 1. Layers

| Layer | What it proves | Where |
|---|---|---|
| Unit | Policy evaluation, masking math, DQ predicates, registry matching | `internal/*` |
| Integration | Full pipeline: registry → PIP → policy → DQ → masking | `integration/` |
| Acceptance (BDD) | Business criteria per journey, executed against the engine and the live Envoy entrypoint | `features/`, `integration/acceptance_test.go` |
| Contract | PIP API shape, schema compatibility | `contracts/`, PIP handler tests |
| Transport | ext_proc message handling, immediate responses, body mutation | `sidecar/extproc` |
| Coverage | Registry ↔ OpenAPI parity | `internal/openapidiff`, `tools/openapi-diff` |
| Bundle | Deterministic render, overlays, digest | `internal/bundle`, `tools/bundle` |
| Conformance | Rendered bundle satisfies delivery fixtures | `internal/conformance`, `tools/conformance` |
| Configuration | Rendered bundle for each service is valid and behaves | delivery pipeline |
| Performance | p99 overhead and CPU per 1k RPS | `bench/` (to be added) |
| Chaos | PIP down, invalid bundle, stale cache | integration + staging |
| Smoke | Real Envoy → ext_proc → engine → app | `hack/envoy-smoke.sh`, `make smoke` |

## 2. Running

```bash
make test        # go test ./... (includes the BDD acceptance suite)
make acceptance  # BDD scenarios against the in-process engine, verbose
make vet         # go vet ./...
make fmt-check   # gofmt check on tracked files
make bench       # integration benchmarks
```

The integration suite starts an in-process PIP over the fixture
`pip/fixtures/attributes.json`; no infrastructure is required.

## 3. Fixture format

Every endpoint in the registry needs a fixture:

```json
{
  "name": "account-owner-masked",
  "request": {
    "method": "GET",
    "path": "/accounts/A123",
    "headers": { "x-subject": "u-1", "x-claim-region": "US" },
    "body": null
  },
  "response": { "status": 200, "body": { "accountNumber": "1234567890" } },
  "expect": {
    "allowed": true,
    "status": 200,
    "maskedBody": { "accountNumber": "******7890" }
  }
}
```

### Required cases per endpoint

1. **Allow + mask** — authorised subject, every masked field asserted exactly.
2. **Deny** — valid subject without entitlement; assert 403 and reason.
3. **Unregistered?** — no; unregistered routes are covered once per service.
4. **Self-description mismatch** — for `selfDescribing` endpoints.
5. **Rejected field** — for write policies with `rejectFields`.
6. **Blocking DQ** — for every blocking rule.
7. **PIP failure** — assert fail-closed 503.
7b. **Staleness** — for fresh/deny-on-stale endpoints, assert 503
    `stale_attributes` when `lagSeconds` exceeds the bound, and allow within it.
8. **Error body masking** — when `maskErrors` is true.
9. **Boundary** — payload at 4 KB; assert masking and latency sanity.

## 4. Gates (must all pass)

- `go test ./...`, `go vet`, `gofmt`.
- `make acceptance`: every scenario in `features/` passes against the engine.
- `make smoke` runs the same `@e2e` scenarios through Envoy (see §9).
- `bundle -check` (or a full render) validates the merged source.
- `conformance` passes every fixture for the service.
- Registry ↔ OpenAPI diff: no undocumented operation, no orphan entry.
- Locked fields not relaxed.
- Every endpoint has fixture cases 1–3 at minimum.
- Conformance suite from `controls-rules` passes against the built sidecar.
- `openapi-diff` reports no missing registry entry and no orphan entry.
- ext_proc adapter tests pass (deny, PIP failure, masking, self-description).
- Performance: p99 control overhead ≤ 3 ms at 1,200 RPS/pod; benchmark trend
  must not regress without justification.
- Live Envoy smoke test passes (`make smoke`, Docker required): allow+mask,
  deny, self-description mismatch, authorized write, unregistered route.

## 5. Adding an endpoint

1. Annotate the OpenAPI operation with `x-resource-type`, `x-resource-id`,
   `x-mask-policy`.
2. Add the registry entry (delivery team owns the merge; service team supplies
   the mapping).
3. Add request/response fixtures, including a mismatch case, and a scenario in
   `features/` for the new journey (allow + mask, deny).
4. Run the delivery pipeline: schema + diff + conformance.
5. Deploy the bundle; verify the sidecar reports the new bundle digest and
   endpoint count.

## 6. Chaos and failure tests

- PIP stopped mid-traffic: expect 503s, no unmasked responses, no 200s.
- PIP latency > timeout: breaker opens, fail closed, recovery after 5 s.
- Corrupt bundle at boot: pod not ready.
- Corrupt bundle on reload: last-known-good retained, alert raised.
- Mask policy removed while endpoint references it: startup validation fails.

## 6b. Bundle and conformance

```bash
bundle -src . -env prod -service payments -out dist/payments
conformance -bundle dist/payments -fixtures tests/fixtures -service payments -v
```

The renderer is deterministic: the same sources produce the same digest.
`manifest.json` carries the digest and counts; Jenkins archives it and the
sidecar logs it per decision. Details: [docs/PIPELINE.md](docs/PIPELINE.md).

## 7. Live Envoy smoke test

`make smoke` builds the binaries, starts the fixture PIP/app/`extproc` on the
host, and runs Envoy `v1.40.0` in Docker with the real `ext_proc` filter chain.
It asserts five behaviours end to end, then runs the BDD `@e2e` criteria against
the same listener, and is the only test that exercises Envoy's own buffering,
`raw_value` pseudo-headers and content-length rules. Requires Docker; CI runs it
after the unit/integration job.

## 8. Performance test plan

`make bench` runs three engine benchmarks on representative 4 KB payloads:
decision without PIP, read path with warm PIP cache, and response masking.
Reference results on an Apple M2: ~7 µs, ~1.5 µs and ~11 µs respectively.

Before enforcing in production, run at 1,200 RPS/pod through Envoy with 4 KB
payloads and record: p50/p99 added latency, CPU per 1k RPS, memory, PIP cache
hit rate and PIP QPS. Store results in the release notes; regressions block
release.

## 9. Acceptance criteria (BDD)

`features/*.feature` holds the business acceptance criteria, one file per
journey. `make acceptance` runs them against the in-process engine; `make smoke`
runs the `@e2e` subset through Envoy via `CONTROLS_ACCEPTANCE_URL`. Scenarios
tagged `@engine` assert internals (decision reasons) and are skipped on the wire.

The definition of done, the step vocabulary and the adoption plan are in
[`docs/BDD.md`](docs/BDD.md). The steward role is
[`docs/agents/conformance-steward.md`](docs/agents/conformance-steward.md).
