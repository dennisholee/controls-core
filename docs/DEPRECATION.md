# Deprecation policy

Status: adopted 2026-11-08. Applies to `controls-core`, `controls-rules`, the
service examples and the demos.

## Rules

1. A deprecated surface stays functional for at least two minor releases after
   the notice, unless it is a security defect (then it is removed immediately).
2. Every deprecation names: replacement, owner, removal target and the test that
   proves the replacement.
3. Production manifests must not enable a deprecated escape hatch. Development
   examples may, with an explicit comment.
4. `make eol-check` fails on EOL bases, old framework/plugin versions and
   deprecated Envoy fields. Warnings alone are not enough.
5. `docs/SUPPORTED-VERSIONS.md` is the supported matrix; this page is the
   removal schedule.

## Current register

| Surface | Status | Replacement | Removal target | Owner |
|---|---|---|---|---|
| Raw `x-subject` / `x-claim-*` | Development-only fallback | Signed `x-controls-identity` envelope | Fallback must be disabled in every non-dev environment now; code path removal after gateway signer rollout | Platform + core |
| `allowUnsigned` / `-allow-unsigned` | Development-only escape | Signed bundle + mounted public key | Remove from Helm values and sample entrypoint once all demo images use signed bundles | Core |
| Legacy sidecar flag mode (`-config`, `-pip`, `-pip-*`, `-opa-*`) | Supported legacy path | `sidecar.yaml` / `CONTROLS_RUNTIME_CONFIG` | Next major after all charts and examples use runtime config | Core |
| OPA 0.70 in `payments-service-demo` | Archived demo only | OPA 1.x, or a maintained OPA example | With the archived demo | Reference service |
| OpenAPI 3.0.3 service contracts | Superseded for the canonical demo | OpenAPI 3.1.0 | Migrated 2026-11-08; internal PIP/test API contracts remain 3.0.3 | Service teams |
| Go 1.27.x | Current | Go 1.28.x | When the toolchain image and CI move together | Core |
| Spring Boot 3.5.x | Removed from active demos | Spring Boot 4.1.x | Done for the canonical demo; archived payments demo is unsupported | Reference service |

## Runtime warnings (since 2026-11-08)

- The sidecar logs a warning when it starts without
  `CONTROLS_IDENTITY_HMAC_KEY` and accepts raw identity headers.
- The sample entrypoint logs a warning when it falls back to legacy flag mode.
- `-allow-unsigned` already logs a warning and must not be used in production.

These warnings do not change enforcement; they make the deprecated path visible
in logs until the removal targets above are met.

## Production requirements

- `CONTROLS_IDENTITY_HMAC_KEY` is set and raw identity headers are stripped by
  the gateway.
- `allowUnsigned: false`, a public key is mounted, and the bundle digest is
  pinned.
- PIP mTLS, caller/field authorization and rate limits are configured.
- The supported-version matrix is current.

## Change process

1. Add the deprecation to this table with an owner and target.
2. Add a warning or failing gate where possible.
3. Update the guide, examples and `docs/SUPPORTED-VERSIONS.md`.
4. Remove only after the notice period and a final `make all` / `guide-smoke`
   pass.
