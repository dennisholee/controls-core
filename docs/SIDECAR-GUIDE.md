# Sidecar adoption guide

Audience: service developers adopting the controls sidecar for an existing
HTTP service, usually Spring Boot.

Outcome: the service keeps its application code, while the sidecar enforces
route-bound entitlement, data quality and response masking before traffic
reaches the service.

**Visual orientation**

- [Runtime architecture](architecture-infographic.svg) — request path, decision flow, bundle pipeline and failure modes.
- [Deployment topology](deployment-infographic.svg) — namespaces, NetworkPolicy, PIP replicas, bundle mounts and delivery.

## 1. What the sidecar does — and does not do

The sidecar:

- matches the route to a registry entry;
- verifies the gateway identity envelope;
- resolves resource attributes from the PIP;
- evaluates entitlement policy and DQ;
- calls the upstream service only after the decision;
- masks the JSON response.

It does not:

- authenticate users — the gateway owns that;
- own business logic or database access;
- store the policy source of truth — delivery owns that;
- replace application authorization for non-data operations.

## 2. Roles

| Area | Owner |
|---|---|
| OpenAPI annotations, resource identity, service code | Service team |
| Registry fragment generation | Service team |
| Entitlement rules, masks, DQ, environment overlays | Delivery team |
| Sidecar/PIP runtime, contracts, Helm charts | Core engineering |
| Gateway identity and header stripping | Platform team |
| Bundle signing and publication | Central pipeline |
| Cluster NetworkPolicies and mTLS | Platform/core |

## 3. Prerequisites

Before starting:

- [ ] gateway authentication, header stripping and a signed identity envelope
- [ ] a PIP endpoint, or a local fixture for development
- [ ] a bundle-signing public key and a place to mount it
- [ ] the `controls-sidecar` Helm library or an equivalent manifest
- [ ] a namespace/Secret for identity, audit salt and PIP client certificates
- [ ] a service OpenAPI 3 contract under source control
- [ ] agreement on who owns masks, rules and DQ

## 4. Phase 1 — annotate the contract

The service owns the route/resource binding. See the runtime path in the
[architecture infographic](architecture-infographic.svg#) before choosing
`x-resource-source`, `x-attributes` and `x-freshness`. Annotate each controlled
operation:

```yaml
/accounts/{accountId}:
  get:
    operationId: getAccount
    x-resource-type: account
    x-resource-id: accountId
    x-resource-source: pip
    x-attributes: [ownerId, region, status]
    x-mask-policy: account.read
    x-mask-errors: true
```

| Extension | Required | Meaning |
|---|---|---|
| `x-resource-type` | yes | PIP namespace and policy/DQ scope |
| `x-resource-id` | yes | path parameter name or body JSON path (`$._id`) |
| `x-mask-policy` | yes | response mask policy name |
| `x-resource-source` | recommended | `pip`, `body`, or `both` |
| `x-attributes` | for PIP | exact attribute projection |
| `x-action` | optional | `read`, `write`, `delete`; otherwise derived |
| `x-freshness` | optional | `cached`, `fresh`, `deny-on-stale` |
| `x-self-describing` | optional | cross-check body `_type`/`_id` |
| `x-mask-errors` | optional | mask JSON error responses |
| `x-max-staleness-seconds` | optional | replica-lag bound |
| `x-controls-ignore` | optional | explicitly omit a route from the generated registry (it remains default-deny) |

Generate the registry fragment:

```bash
openapi-registry \
  -openapi services/payments/openapi.yaml \
  -service payments \
  -out services/payments/controls/registry.json
```

Gate it:

```bash
openapi-registry -check ...   # no drift
openapi-diff ...              # coverage and annotation parity
```

The fragment is service-owned; do not hand-edit it in `controls-rules`.

## 5. Phase 2 — run locally

A typical local layout:

```text
services/payments/
  openapi.yaml
  controls/
    registry.json       # generated
    rules.json          # local fixture
    mask-policies.json
    dq.json
  sidecar.yaml
  pip/pip.yaml
  envoy/envoy.yaml
  compose.yaml
```

Minimal `sidecar.yaml`:

```yaml
server:
  listen: ":50051"
  metricsAddr: ":9090"
bundle:
  path: /etc/controls/bundle
  allowUnsigned: true          # local only
  reloadInterval: 30s
pip:
  apiVersion: "1.0"
  url: http://pip:8082
  timeout: 100ms
audit:
  sample: 1
  buffer: 0
  salt: local-dev-salt
```

Minimal `pip/pip.yaml`:

```yaml
server:
  addr: ":8082"
  metricsAddr: ":9090"
repository:
  fixtures: /etc/controls/attributes.json
```

Run the sample or compose stack:

```bash
make -C examples/accounts-read test
make up
curl -s -H 'x-subject: u-1' http://127.0.0.1:18000/accounts/A123
```

The service application is not published; only the sidecar listener is exposed.

## 6. Phase 3 — policy and DQ ownership

The service names policies. Delivery authors them.

| File | Owner | Purpose |
|---|---|---|
| `registry.json` | service | route binding, identity, mask-policy name |
| `rules.json` | delivery | entitlement policy or OPA/Cedar selector |
| `mask-policies.json` | delivery | field modes, locked fields, reject fields |
| `dq.json` | delivery | request validation and blocking rules |

Local fixtures are for development only. Production bundles come from the
central `controls-rules` pipeline and are signed.

## 7. Phase 4 — package and deploy

No application code changes are required. See the
[deployment topology](deployment-infographic.svg#) for the namespace,
NetworkPolicy and PIP placement this phase must match. The pod contains:

- the app container;
- the Envoy container;
- the ext_proc container.

Use the Helm library:

```yaml
containers:
  - name: app
    image: ...
  {{- include "controls-sidecar.containers" . | nindent 8 }}
volumes:
  {{- include "controls-sidecar.volumes" . | nindent 8 }}
```

Preferred: set `extproc.runtimeConfig=true`, include
`controls-sidecar.configmap`, and mount the generated `sidecar.yaml`. The
legacy flag mode remains supported.

Secrets mounted by the parent:

- audit salt;
- identity HMAC key;
- bundle public key;
- PIP client CA/cert/key.

Rules:

- only the sidecar port is exposed;
- the app port is never published;
- a NetworkPolicy admits the gateway only to the sidecar;
- the sidecar reaches the PIP over mTLS;
- metrics are admitted only from monitoring.

## 8. Phase 5 — identity and PIP

With `CONTROLS_IDENTITY_HMAC_KEY` set, the sidecar requires the signed
`x-controls-identity` envelope. Raw `x-subject`/`x-claim-*` are development
only.

The sidecar checks the PIP contract at startup:

```yaml
pip:
  apiVersion: "1.0"
```

A mismatch keeps the sidecar unready and exposes
`controls_pip_version_mismatch_total`.

Production PIP sources are configured in `sources.json`:

```json
{
  "sources": {
    "payment": {
      "url": "https://payments-read.internal/payments/{id}",
      "fields": ["ownerId", "status"],
      "timeoutMs": 200,
      "caFile": "/etc/controls-tls/ca.pem",
      "certFile": "/etc/controls-tls/client.pem",
      "keyFile": "/etc/controls-tls/client-key.pem"
    }
  }
}
```

Attributes must be flat JSON and match the registry projection.

## 9. Phase 6 — test

| Test | What it proves |
|---|---|
| `mvn verify` | app builds and registry drift test passes |
| `openapi-registry -check` | registry matches annotations |
| `openapi-diff` | every controlled operation is annotated |
| `controls-check` | rules/masks/DQ/attributes resolve |
| contract tests through compose | allow, deny, masking, DQ, 404, PIP outage |
| `make smoke` (platform) | real Envoy + ext_proc path |
| failure tests | PIP outage, OPA error, stale/unknown lag, bad bundle |

Minimum end-to-end matrix:

1. owner request → allowed and masked;
2. different requestor → denied;
3. missing identity → denied;
4. blocking DQ → denied;
5. rejected field (`pan`) → denied;
6. unknown resource → 404;
7. PIP down → 503;
8. PIP version mismatch → not ready;
9. upstream port not reachable directly.

Do not test only the engine; exercise the sidecar through Envoy.

## 10. Phase 7 — rollout

1. Generate and review the registry fragment.
2. Merge delivery policy and render the bundle.
3. Run conformance and schema gates.
4. Sign and publish the bundle@digest.
5. Deploy the sidecar with the signed bundle public key.
6. Canary one pod/service with a shadow or small traffic slice.
7. Check deny/mask/DQ/audit metrics.
8. Roll out the rest.
9. Record the bundle digest and rollback digest.

Order matters: PIP → sidecar → bundle. A sidecar with a mismatched PIP contract
stays unready rather than serving wrong data.

## 11. Phase 8 — operate

| Signal | Alert |
|---|---|
| `controls_decisions_total{reason="pip_unavailable"}` | PIP error rate |
| `controls_pip_cache_total{cache="hit"}` | cache hit rate |
| `controls_stale_denials_total` / `lag_unknown` | replica lag |
| `controls_dq_violations_total{blocking="true"}` | DQ spike |
| `controls_pip_version_mismatch_total` | contract mismatch |
| `controls_bundle_info` | bundle version/digest |
| audit write/drop counters | audit pipeline |

Reload is poll/SIGHUP: verify signature, atomic swap, keep last-known-good.
Rollback is redeploying the previous bundle digest.

## 12. Troubleshooting

| Symptom | First check |
|---|---|
| `403 default:deny` | registry route, policy rules, subject claims |
| `403 dq_blocking:*` | request body and DQ rule |
| `403 rejected_field:*` | mask policy `rejectFields` |
| `503 pip_unavailable` | PIP Service, NetworkPolicy, mTLS, timeout |
| `503 lag_unknown` / `stale_attributes` | PIP lag header, staleness bound |
| `404 resource_not_found` | registry resource id and PIP data |
| response not masked | content type, mask policy, non-JSON path |
| sidecar not ready | PIP version, bundle signature, audit salt |
| empty/500 response | missing mask policy or non-JSON on masked route |

## 13. Anti-patterns

- exposing the app port directly;
- trusting client `x-subject`/`x-claim-*`;
- authoring production rules in the service repo;
- hand-editing the registry fragment;
- one PIP deployment per service;
- shipping `allowUnsigned` or an empty bundle key;
- testing only the engine, not Envoy;
- putting business logic in the sidecar;
- treating OPA policy as part of the signed bundle.

## 14. Definition of done

- [ ] annotations complete; generated registry committed
- [ ] `mvn verify`, `openapi-diff`, `controls-check` green
- [ ] signed bundle and public key mounted
- [ ] gateway identity envelope enforced
- [ ] app port unpublished; NetworkPolicy applied
- [ ] PIP mTLS/caller checks enabled
- [ ] contract tests cover allow, deny, mask, DQ, 404, outage
- [ ] canary metrics reviewed
- [ ] dashboards/alerts for PIP, DQ, stale, audit, version mismatch
- [ ] bundle digest and rollback digest recorded

## 15. Reference

- [`ARCHITECTURE.md`](../ARCHITECTURE.md)
- [Architecture infographic](architecture-infographic.svg)
- [Deployment infographic](deployment-infographic.svg)
- [`docs/PIPELINE.md`](PIPELINE.md)
- [`docs/COMPATIBILITY.md`](COMPATIBILITY.md)
- [`contracts/`](../contracts/)
- [`deploy/helm/controls-sidecar`](../deploy/helm/controls-sidecar)
- [`examples/`](../examples/)
- [`RUNBOOK.md`](../RUNBOOK.md)
