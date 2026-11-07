# Sidecar adoption guide

Audience: service teams putting an existing Spring Boot (or other HTTP) service
behind the controls sidecar.

## What changes in the service

Nothing in application code. The sidecar enforces route binding, entitlement,
DQ and response masking before/after the app.

The service must be compatible with these constraints:

- JSON responses on masked routes; non-JSON fails closed unless explicitly
  allowed.
- No SSE/streaming on masked routes; compression is stripped by the sidecar.
- Request and response bodies ≤ 1 MiB by default.
- The app must not be reachable except through the sidecar.
- The app must not trust client-supplied identity headers.

## 1. Contract

Add a source OpenAPI 3 contract and bind each controlled operation:

```yaml
get:
  x-resource-type: account
  x-resource-id: accountId
  x-mask-policy: account.read
  x-resource-source: pip
  x-attributes: [ownerId, region, status]
```

Generate the registry fragment:

```bash
openapi-registry -openapi openapi.yaml -service accounts -out controls/registry.json
```

The service owns routes, schemas, resource identity and the mask-policy name.
Delivery owns the rules, mask modes and DQ.

## 2. Controls

For local development, keep a minimal fixture under `controls/`:

```
registry.json
rules.json
mask-policies.json
dq.json
```

For production, do not copy rules into the service repo. The central pipeline
merges the generated registry fragment with `controls-rules`, validates, signs
and publishes the bundle.

## 3. Deployment

Use the `controls-sidecar` Helm library or an equivalent manifest to add:

- Envoy container on the service port;
- ext_proc container on `127.0.0.1:50051`;
- ConfigMap: `envoy.yaml` and the signed bundle;
- Secret: audit salt, identity HMAC key, bundle public key, PIP client certs;
- env: `PIP_URL`, optional `PIP_CA`/`PIP_CERT`/`PIP_KEY`/`PIP_SERVER_NAME`,
  `OPA_URL` if using Rego.

The service container keeps listening on its normal port; only the sidecar port
is exposed.

## 4. Identity

The gateway authenticates and injects identity. With
`CONTROLS_IDENTITY_HMAC_KEY`, the sidecar requires the signed
`x-controls-identity` envelope and ignores raw `x-subject`/`x-claim-*`.
Raw headers are development-only.

## 5. Test

```bash
mvn verify                     # app + Java registry drift test
openapi-diff ...               # coverage/annotation gate
controls-check ...             # rules/DQ/masks vs registry + OpenAPI
make -C examples/accounts-read test   # reference sidecar behaviour
```

Add contract tests for allow, deny, masking, DQ and PIP outage through the
sidecar, not just the engine.

## 6. Operate

- Scrape sidecar `:9090/metrics`; ship audit (`stdout` or `-audit-out`).
- Watch PIP cache hit rate, lag, stale/unknown lag denials, DQ violations.
- Reload is poll/SIGHUP: verify, atomic swap, last-known-good.
- Alerts: PIP error rate/latency, stale/lag denials, DQ block spike, reload
  failure, audit drops.

## 7. Anti-patterns

- Exposing the app port directly.
- Trusting client `x-subject`/`x-claim-*`.
- Per-service PIP deployments.
- Hand-editing rendered bundles.
- Shipping `-allow-unsigned` or an empty bundle pubkey.
- Putting business logic in the sidecar.
- Testing only the engine; masking/DQ must be exercised through Envoy.

## Checklist

- [ ] OpenAPI annotations complete; registry generated and committed
- [ ] drift + controls-check gates green
- [ ] rules/masks/DQ owned by delivery
- [ ] gateway identity and header stripping verified
- [ ] app port unpublished; NetworkPolicy applied
- [ ] signed bundle + pubkey; no `-allow-unsigned`
- [ ] PIP mTLS/caller checks enabled where required
- [ ] contract tests: allow, deny, mask, DQ, PIP outage
- [ ] metrics/audit dashboards and alerts
- [ ] rollback bundle digest recorded
