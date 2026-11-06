# Testing

The controls plane is configuration-driven, so tests must prove both the engine
and **the effective configuration**. Conformance is a pipeline gate, not a
manual review step.

## 1. Layers

| Layer | What it proves | Where |
|---|---|---|
| Unit | Policy evaluation, masking math, DQ predicates, registry matching | `internal/*` |
| Integration | Full pipeline: registry → PIP → policy → DQ → masking | `integration/` |
| Contract | PIP API shape, schema compatibility | `contracts/`, PIP handler tests |
| Configuration | Rendered bundle for each service is valid and behaves | delivery pipeline |
| Performance | p99 overhead and CPU per 1k RPS | `bench/` (to be added) |
| Chaos | PIP down, invalid bundle, stale cache | integration + staging |

## 2. Running

```bash
make test        # go test ./...
make vet         # go vet ./...
make fmt-check   # gofmt -l
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
8. **Error body masking** — when `maskErrors` is true.
9. **Boundary** — payload at 4 KB; assert masking and latency sanity.

## 4. Gates (must all pass)

- `go test ./...`, `go vet`, `gofmt`.
- JSON Schema validation of the rendered bundle.
- Registry ↔ OpenAPI diff: no undocumented operation, no orphan entry.
- Locked fields not relaxed.
- Every endpoint has fixture cases 1–3 at minimum.
- Conformance suite from `controls-rules` passes against the built sidecar.
- Performance: p99 control overhead ≤ 3 ms at 1,200 RPS/pod.

## 5. Adding an endpoint

1. Annotate the OpenAPI operation with `x-resource-type`, `x-resource-id`,
   `x-mask-policy`.
2. Add the registry entry (delivery team owns the merge; service team supplies
   the mapping).
3. Add request/response fixtures, including a mismatch case.
4. Run the delivery pipeline: schema + diff + conformance.
5. Deploy the bundle; verify the sidecar reports the new bundle digest and
   endpoint count.

## 6. Chaos and failure tests

- PIP stopped mid-traffic: expect 503s, no unmasked responses, no 200s.
- PIP latency > timeout: breaker opens, fail closed, recovery after 5 s.
- Corrupt bundle at boot: pod not ready.
- Corrupt bundle on reload: last-known-good retained, alert raised.
- Mask policy removed while endpoint references it: startup validation fails.

## 7. Performance test plan

Before enforcing in production, run at 1,200 RPS/pod with 4 KB payloads and
record: p50/p99 added latency, CPU per 1k RPS, memory, PIP cache hit rate and
PIP QPS. Store results in the release notes; regressions block release.
