# ADR-0012 — External OPA decision engine

Status: accepted (2026-11-07)

## Context

Some delivery teams already run OPA/Rego and want to reuse those policies. An
in-process OPA dependency is large, and compiling Rego to Wasm adds toolchain
complexity. OPA exposes a stable data API, and a local OPA can be reached over
loopback or the mesh.

## Decision

- The bundle selects `rules.engine: "opa"` and a decision path
  (`rules.opa.decision`, default `controls/authz/allow`).
- The sidecar supplies the base URL (`-opa-url` / `CONTROLS_OPA_URL`) so the
  same bundle runs in every environment.
- Each decision is a `POST /v1/data/<decision>` with
  `{"input": {subject, resource, resourceId, action, request}}`.
- `true` or `{"allow": true}` allows; anything else denies. HTTP errors,
  timeouts, malformed responses and non-200 statuses **fail closed** with
  `opa:error`.
- No decision cache. OPA must run next to the sidecar; the 100 ms default
  timeout is the outer bound.

## Consequences

- Rego teams can adopt the controls plane without rewriting policies.
- OPA policies are outside the signed bundle; their versioning and review are
  owned by the OPA deployment. The decision path is in the signed bundle.
- The sidecar depends on OPA availability for endpoints using this engine.
- OPA traffic must be network-restricted and authenticated at the mesh; the
  adapter itself has no credentials.

## Alternatives

- Embed OPA: heavy binary and data model.
- Rego → Wasm: extra build tooling, smaller runtime.
- Cedar: preferred default alternative for new policies.
