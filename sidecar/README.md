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

## Production adapter

`cmd/extproc` runs the Envoy `ext_proc` gRPC service; the adapter is in
`extproc/` and the Envoy configuration is in [`envoy/`](envoy/README.md).

```bash
CONTROLS_AUDIT_SALT=dev-salt go run ./sidecar/cmd/extproc -listen :50051 \
  -config integration/testdata/config \
  -pip http://127.0.0.1:8082
```

- `BUFFERED` request and response body modes; chunked bodies are tolerated.
- Immediate responses for deny (403/404/503), body mutation for masking.
- `failure_mode_allow: false` and a 1 MiB body cap by default.
- Strip `Accept-Encoding` on JSON routes; non-JSON passes through unchanged and
  must be handled at the gateway.
- Pseudo-headers and the mutated `content-length` are read/written as
  `raw_value`, per Envoy's wire behaviour.
- `/metrics`, `/healthz` and `/readyz` are served on `:9090`; decision audit is
  JSON on stdout (hashed identifiers, denials always, allows sampled).
- `-bundle-pubkey` (or `CONTROLS_BUNDLE_PUBKEY`) enables Ed25519 verification of
  `manifest.sig`; startup fails if the bundle was modified or unsigned.

The adapter is exercised against a live Envoy 1.40 by `make smoke`
([`hack/envoy-smoke.sh`](../hack/envoy-smoke.sh)).

## Performance expectations

At 1,000 RPS/pod with 4 KB bodies: <1.5 ms p99 overhead on cache hit, <7 ms on
PIP miss. See [`../ARCHITECTURE.md`](../ARCHITECTURE.md) for the budget and
[`../TESTING.md`](../TESTING.md) for the gate.
