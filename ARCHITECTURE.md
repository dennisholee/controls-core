# Architecture

Status: reference implementation `v0.1.0`.

## 1. Context and constraints

| Constraint | Value | Consequence |
|---|---|---|
| Traffic | north–south, 5,000 RPS | one edge enforcement point; no mesh required |
| Pods | ≤ 5 | ~1,000 RPS/pod; sidecar restarts cost 20% capacity |
| Payload | 2 KB typical, 4 KB p99 | full body buffering is cheap; no streaming |
| Latency budget | p99 1 s end to end | controls must stay under ~3–5 ms p99 |
| Fleet | JVM-only, but centrally managed | sidecar keeps policy/engine out of app releases |
| Entitlements | request-level (gateway) + data-level (payload + DB state) | data-level must run where payload and PIP meet |

## 2. Components

```
                     ┌────────────────────────────── control plane ─────────────────────────────┐
                     │ PIP HTTP API (2+ replicas) · read replicas · classification cache        │
                     │ bundle/config distribution · decision log pipeline                       │
                     └──────────────────────────────────────────────────────────────────────────┘
 client ─▶ gateway ─▶ ┌─ Envoy sidecar (per pod) ─────────────┐ ─▶ Spring Boot service
          (authn +    │ ext_authz  coarse/defence-in-depth     │
           coarse     │ ext_proc   request  → registry         │
           authz)     │            → PIP (cached)              │
                      │            → policy eval → DQ          │
                      │            response → mask             │
                      └───────────────────────────────────────┘
```

| Component | Code | Responsibility |
|---|---|---|
| Engine | `internal/engine` | Orchestrates match → identity → attributes → decision → DQ → masking |
| Registry | `internal/config` | Route binding, identity extraction, authority rule |
| Policy | `internal/policy` | Deny-first evaluation; pluggable interface for OPA/Cedar |
| Masking | `internal/mask` | Full/partial field masking, array paths, locked fields |
| DQ | `internal/dq` | Multi-state validation; blocking only when configured |
| PIP client | `internal/pipclient` | HTTP, projection, TTL cache, coalescing, breaker, fail-closed |
| PIP server | `pip/server` | Batch/classification API over a read-only Repository |
| Harness | `sidecar/cmd/sidecar` | HTTP harness used by tests; production adapter is ext_proc |

## 3. Request flow

1. **Gateway** authenticates, authorizes the route/scope, and injects verified
   `x-subject`/claim headers. Client-supplied equivalents are stripped.
2. **Registry match.** No match ⇒ `defaultUnregistered` (production: deny).
3. **Self-description cross-check.** With `selfDescribing`, payload `_type`
   must equal the registry `resourceType` and `_id` must equal the derived
   identifier. Mismatch ⇒ 403. *The route is the authority, not the payload.*
4. **Resource identity** from path parameter or body JSON path.
5. **Rejected fields.** Write policies reject fields such as `pan`/`cvv`.
6. **Resource attributes.** `resourceSource` selects body, PIP, or both. PIP
   calls request only the registry's `attributes` projection.
7. **Policy.** Deny rules are absolute and evaluated first; allow rules are
   OR-ed; no match falls back to `defaultEffect` (deny).
8. **DQ.** Violations are recorded; only `blocking: true` rejects.
9. **Response.** The matched endpoint's mask policy is applied to JSON
   responses, including errors when `maskErrors` is true.

## 4. Latency and capacity budget

At 1,000 RPS/pod with 4 KB p99:

| Step | p99 (est.) | Notes |
|---|---|---|
| ext_proc buffered round trip | 0.3–1.0 ms | body copied once |
| registry + policy eval | 0.05–0.2 ms | in-process, no I/O |
| PIP cache hit | ~0.1 ms | Caffeine-style local cache |
| PIP cache miss | 2–5 ms | HTTP + replica query |
| masking (4 KB) | 0.05–0.15 ms | parse + mutate + serialize |
| **Total, cache hit** | **< 1.5 ms** | well inside budget |
| **Total, cache miss** | **< 7 ms** | still <1% of p99 |

CPU: roughly 0.1–0.3 vCPU per 1,000 RPS for the sidecar. At 5 pods, budget ~2
vCPU and ~250 MB across the fleet for controls.

Cache requirements: hit rate >95% steady state; miss storm protection via
per-key coalescing; TTL 30–60 s for attributes, hours for classification. The
cache is keyed by type, id and field projection.

## 5. Failure behaviour

| Failure | Behaviour |
|---|---|
| PIP timeout / unavailable | **Deny** (503 `pip_unavailable`) — fail closed |
| PIP resource not found | 404 `resource_not_found` |
| Invalid bundle at boot | Pod does not become ready |
| Invalid bundle on reload | Keep last-known-good, alert |
| Policy bundle missing/corrupt | Deny on next start; never run partially configured |
| Mask policy missing at runtime | Deny rather than return unmasked |
| Unregistered route | Deny (`defaultUnregistered: deny`) |
| DQ violation, non-blocking | Allow; log + metric |
| DQ violation, blocking | 403 `dq_blocking:<rule>` |

## 6. Deployment

- **Sidecar**: one image, injected into all 5 service pods. `maxUnavailable: 1`.
  Upgrades are rolling pod restarts; no application rebuild.
- **PIP**: 2+ replicas behind a Service, mTLS, read-only DB credentials,
  connection pool sized for cache-miss QPS (~5% of 5,000 = 250 RPS).
- **Bundle**: rendered by the delivery pipeline, signed, digest-pinned, mounted
  as a ConfigMap/volume. Sidecar records the bundle digest in the decision log.

## 7. ext_proc adapter (next increment)

Envoy configuration to be added under `sidecar/envoy/`:

- `ext_proc` with `request_body_mode: BUFFERED`, `response_body_mode: BUFFERED`.
  At 4 KB p99 this is simpler and more debuggable than streamed modes.
- Request phase: map HTTP request → `engine.Request`; call `ProcessRequest`;
  return `ImmediateResponse` (403/503) or continue.
- Response phase: call `MaskResponse`; return the mutated body with corrected
  content length.
- **Disable compression** for JSON routes (`Accept-Encoding` stripped) to avoid
  decompress/recompress cost; 4 KB does not benefit from gzip.
- Explicitly bypass SSE/streaming endpoints and audit the bypass.
- `ext_authz` remains available for coarse checks but is expected to be a no-op
  while the gateway owns route authz.

## 8. Known gaps

| Gap | Impact | Plan |
|---|---|---|
| ext_proc adapter not implemented | Harness only; not deployable to Envoy yet | Next increment |
| `freshness` validated but not enforced | Stale reads possible on `fresh` journeys | PIP lag metrics + enforcement |
| Classification-driven mask-all fallback not wired | Fail-safe masking relies on policy defaults | Wire classification cache into mask |
| Reference rule engine, not OPA/Cedar | Less expressive than Rego/Cedar | Swap behind `policy.Evaluator` |
| Registry/OpenAPI diff gate not in CI | Unconfigured routes possible | Add gate to delivery pipeline |
| Decision log emitted to stdout only | No audit sink yet | OTel → collector |
