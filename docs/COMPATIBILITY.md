# Compatibility

Artifacts are versioned independently but released together from
`controls-core` (sidecar, PIP) and `controls-rules` (configuration).

## 1. Current matrix

| Sidecar | Contracts | PIP API | Rule schema | Notes |
|---|---|---|---|---|
| 0.1.x | 1.0.x | 1.0.x | 1.0.x | Reference implementation |

## 2. Support policy

- The current minor and the previous minor are supported.
- A sidecar refuses to start if the bundle declares a contract major it does
  not support; it warns on a newer minor it does not recognise.
- A PIP may serve a newer minor to an older sidecar as long as fields are
  additive. Unknown response fields are ignored by the client.
- Deprecations are announced one minor ahead and removed at the next major.

## 3. Upgrade order

1. **Contracts** (publish the package).
2. **PIP** (deploy; additive changes first).
3. **Sidecar** (rolling restart; `maxUnavailable: 1`).
4. **Rules** (signed bundle referencing the new contract version).

Rollback order is the reverse. A bundle may only reference a contract version
supported by the deployed sidecar.

## 4. Compatibility rules

- Additive fields and enum values: minor.
- Removing/renaming a field, changing a default, or turning advisory behaviour
  into enforcement: major.
- Mask semantics are part of the contract: changing partial-masking output is a
  major change, because downstream tests and consumer expectations depend on the
  exact masked value.
- Locked fields cannot change without a security-reviewed major release.

## 5. Rule schema declaration

Every rendered bundle declares `schemaVersion` and `bundleVersion`. The sidecar
logs both at startup and includes the digest in every decision log line, so an
incident can always be tied to an exact configuration.
