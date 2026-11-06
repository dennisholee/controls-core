# ADR-0003 — Envoy `ext_proc` with buffered bodies

Status: accepted (2026-11-06)

## Context

Data-level entitlement needs the request payload; masking needs response
mutation. `ext_authz` can only allow/deny on request attributes. Payload p99 is
4 KB.

## Decision

Use Envoy's External Processing filter (`ext_proc`) with `BUFFERED` request and
response body modes. Keep `ext_authz` available for coarse checks but expect it
to be a no-op while the gateway owns route authorization.

## Consequences

- One component handles extraction, decisions, DQ and response masking.
- Buffering 4 KB is cheap; no streaming complexity or partial-body heuristics.
- Compression is stripped for JSON routes to avoid decompress/recompress cost.
- Streaming/SSE endpoints cannot be buffered and must be explicitly excluded
  and audited.

## Alternatives

- `ext_authz` with body: no response mutation, so masking would need a second
  mechanism.
- `ext_proc` streamed: unnecessary complexity at 4 KB; harder to test and debug.
- Envoy Wasm filter: lower hop cost, but harder policy maintenance and tooling
  at this scale.
