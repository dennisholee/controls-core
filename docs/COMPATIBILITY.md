# Compatibility

Artifacts are versioned independently but released together from
`controls-core` (sidecar, PIP) and `controls-rules` (configuration).

## 1. Current matrix

| Sidecar | Contracts | PIP API | Rule schema | Notes |
|---|---|---|---|---|
| 0.39.x | 1.0.x | 1.0.x | 1.0.x | adoption gate fixes, DQ action/endpoint scope, array DQ, PIP fixture metadata |
| 0.38.x | 1.0.x | 1.0.x | 1.0.x | PIP API handshake, runtime config |
| 0.22.x | 1.0.x | 1.0.x | 1.0.x | source-aware readiness |
| 0.21.x | 1.0.x | 1.0.x | 1.0.x | signed manifest, rate limits |
| 0.20.x | 1.0.x | 1.0.x | 1.0.x | async audit, contract major |
| 0.19.x | 1.0.x | 1.0.x | 1.0.x | identity envelope, mandatory signing |
| 0.18.x | 1.0.x | 1.0.x | 1.0.x | breaker/cache fixes |
| 0.17.x | 1.0.x | 1.0.x | 1.0.x | security hardening |
| 0.16.x | 1.0.x | 1.0.x | 1.0.x | PIP per-caller authz |
| 0.15.x | 1.0.x | 1.0.x | 1.0.x | PIP mTLS |
| 0.14.x | 1.0.x | 1.0.x | 1.0.x | PIP concurrency limit |
| 0.13.x | 1.0.x | 1.0.x | 1.0.x | PIP metrics |
| 0.12.x | 1.0.x | 1.0.x | 1.0.x | HTTP attribute sources |
| 0.11.x | 1.0.x | 1.0.x | 1.0.x | load-test gate |
| 0.10.x | 1.0.x | 1.0.x | 1.0.x | OPA engine |
| 0.9.x | 1.0.x | 1.0.x | 1.0.x | bundle reload, non-JSON fail-closed |
| 0.8.x | 1.0.x | 1.0.x | 1.0.x | Cedar policy engine |
| 0.7.x | 1.0.x | 1.0.x | 1.0.x | Helm deployment controls |
| 0.6.x | 1.0.x | 1.0.x | 1.0.x | bundle signing |
| 0.5.x | 1.0.x | 1.0.x | 1.0.x | classification fallback |
| 0.4.x | 1.0.x | 1.0.x | 1.0.x | audit, metrics |
| 0.3.x | 1.0.x | 1.0.x | 1.0.x | bundle renderer, conformance, freshness |
| 0.2.x | 1.0.x | 1.0.x | 1.0.x | ext_proc adapter, OpenAPI gate |
| 0.1.x | 1.0.x | 1.0.x | 1.0.x | HTTP harness only |

## 2. Support policy

- The current minor and the previous minor are supported.
- A sidecar refuses to start if the bundle declares a contract major it does
  not support; it warns on a newer minor it does not recognise.
- A PIP may serve a newer minor to an older sidecar as long as fields are
  additive. Unknown response fields are ignored by the client.
- Deprecations are announced one minor ahead and removed at the next major.

## 3. PIP API handshake

The sidecar calls `GET /version` at startup and compares `pip.apiVersion`
(major/minor). A mismatch or unreachable PIP fails startup. The PIP server test
pins `APIVersion` to `contracts/pip-api.yaml`; run `make pip-api-compat` to
reject breaking OpenAPI changes.

## 4. Upgrade order

1. **Contracts** (publish the package).
2. **PIP** (deploy; additive changes first).
3. **Sidecar** (rolling restart; `maxUnavailable: 1`).
4. **Rules** (signed bundle referencing the new contract version).

Rollback order is the reverse. A bundle may only reference a contract version
supported by the deployed sidecar.

## 5. Compatibility rules

- Additive fields and enum values: minor.
- Removing/renaming a field, changing a default, or turning advisory behaviour
  into enforcement: major.
- Mask semantics are part of the contract: changing partial-masking output is a
  major change, because downstream tests and consumer expectations depend on the
  exact masked value.
- Locked fields cannot change without a security-reviewed major release.

## 6. Rule schema declaration

Every rendered bundle declares `schemaVersion` and `bundleVersion`. The sidecar
logs both at startup and includes the digest in every decision log line, so an
incident can always be tied to an exact configuration.
