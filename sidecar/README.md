# sidecar

The centrally managed enforcement point. One image, injected into every service
pod. It contains the Envoy filter chain and the `ext_proc` service that runs the
controls engine.

## How it works

```
Envoy
 ├─ ext_authz   coarse checks (gateway owns route authz; usually a no-op)
 └─ ext_proc
      request  BUFFERED → registry match → identity cross-check
               → PIP attributes (cached) → policy → DQ
      response BUFFERED → mask policy → mutated body
```

The engine lives in [`../internal/engine`](../internal/engine). The transport
adapter is intentionally thin so the engine can be tested without Envoy.

## Local harness

`cmd/sidecar` exposes the same engine over HTTP:

```bash
go run ./sidecar/cmd/sidecar \
  -config integration/testdata/config \
  -pip http://127.0.0.1:8082
```

`POST /v1/process` takes a request/response envelope and returns the decision
plus the masked response. Subject and claims come from `x-subject` and
`x-claim-*` headers — **harness only**; production identity comes from the
gateway trust chain.

## Configuration injection

The pod mounts the rendered bundle (registry, rules, mask policies, DQ) from the
delivery pipeline, and passes its path via `-config`. The sidecar validates on
load and refuses to start on an invalid bundle. Reload semantics: validate,
atomic swap, keep last-known-good on failure.

## Production adapter (next)

- `envoy/ext_proc` with `request_body_mode: BUFFERED`,
  `response_body_mode: BUFFERED`.
- Strip `Accept-Encoding` on JSON routes; pass through non-JSON with an explicit
  bypass audit record.
- Convert engine results to `ImmediateResponse` (403/503) or continue.
- Add the request id to the decision log and propagate it to the app.

## Performance expectations

At 1,000 RPS/pod with 4 KB bodies: <1.5 ms p99 overhead on cache hit, <7 ms on
PIP miss. See [`../ARCHITECTURE.md`](../ARCHITECTURE.md) for the budget and
[`../TESTING.md`](../TESTING.md) for the gate.
