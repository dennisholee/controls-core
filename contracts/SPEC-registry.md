# SPEC — Endpoint Registry

Status: normative. Schema: [`registry.schema.json`](registry.schema.json).
Version: 1.0.

The registry is the single source of truth for **which policy applies to which
route**. It is configuration, owned by the delivery team, generated and
validated by the pipeline, and injected into the sidecar at deploy time.

## 1. Authority rule

> **Resource type and resource identity are bound to the route. Payload fields
> are data, never authority.**

A request body may be self-describing (`_type`, `_id`), but those values are
only cross-checked against the registry entry. A mismatch rejects the request
(`self_describing_type_mismatch` / `self_describing_id_mismatch`, HTTP 403).
This prevents a caller from selecting the policy that applies to them.

## 2. Matching

- Method match is case-insensitive.
- Path match is **exact, segment-wise**. `{name}` captures exactly one non-empty
  segment. Wildcards, regex and prefix matching are not permitted.
- At most one endpoint may match a request. Duplicate `id` values are a
  validation error; overlapping method+path templates are a configuration error
  caught by the registry/OpenAPI diff gate.
- Unmatched routes follow `defaultUnregistered`. Production MUST set `deny`.
  `allow` exists only for migration and is rejected by the conformance gate for
  namespaces containing classified resource types.

## 3. Fields

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | Stable identifier used in decision logs. `kebab-case`. |
| `method` | yes | HTTP method. |
| `path` | yes | Path template. |
| `resourceType` | yes | Type passed to policy and PIP. |
| `action` | no | `read`/`write`/`delete`; derived from method when absent. |
| `resourceId` | yes | Where the instance identifier lives: `{from: path, name}` or `{from: body, path}`. |
| `resourceSource` | no | `pip` (default), `body`, `both`. |
| `attributes` | no | Field projection requested from the PIP. |
| `maskPolicy` | no | Key in the mask-policies document. |
| `maskErrors` | no | Apply masking to error responses (default `true`). |
| `freshness` | no | `cached` (default), `fresh`, `deny-on-stale`. |
| `selfDescribing` | no | Require payload `_type`/`_id` to match this entry. |

### resourceSource

- `pip` — resource exists before the call; attributes come from the Attribute
  Service. A miss is `404 resource_not_found`.
- `body` — resource is being created; attributes come from the request body.
  The PIP is not called.
- `both` — payload overlaid with PIP attributes; used when the request carries
  some fields and the policy needs authoritative ones.

### freshness

- `cached` — sidecar cache and PIP cache may serve the value.
- `fresh` — bypass sidecar cache; PIP must serve a non-stale value.
- `deny-on-stale` — deny when replica lag or attribute version exceeds the
  journey threshold.

Reference implementation note: `fresh`/`deny-on-stale` are reserved and not yet
enforced by the reference engine; they are validated so that configuration can
be authored ahead of the implementation. Enforcement lands with the PIP lag
metrics (see ADR-0004).

## 4. Examples

Resource read:

```json
{
  "id": "get-account",
  "method": "GET",
  "path": "/accounts/{accountId}",
  "resourceType": "account",
  "action": "read",
  "resourceId": { "from": "path", "name": "accountId" },
  "resourceSource": "pip",
  "attributes": ["ownerId", "region", "status"],
  "maskPolicy": "journey.account.read",
  "maskErrors": true,
  "freshness": "cached",
  "selfDescribing": false
}
```

Resource creation with a self-describing payload:

```json
{
  "id": "create-payment",
  "method": "POST",
  "path": "/payments",
  "resourceType": "payment",
  "action": "write",
  "resourceId": { "from": "body", "path": "$._id" },
  "resourceSource": "body",
  "maskPolicy": "journey.payment.write",
  "selfDescribing": true
}
```

## 5. Validation gates

1. JSON Schema validation (`registry.schema.json`).
2. Semantic validation: unique ids, resolvable `maskPolicy`, valid
   `resourceId` shape, `resourceType` present in the PIP catalog.
3. **OpenAPI diff**: every operation in the service's OpenAPI document has a
   registry entry, and every registry entry maps to a documented operation.
   This is the gate that prevents an unconfigured route from shipping.
4. **Fixture tests**: a request/response sample per endpoint, including a
   `_type` mismatch case, must produce the expected decision.

## 6. Versioning

Additive fields bump the minor contract version. Removing or renaming a field,
or changing default behaviour, bumps the major version and requires a
compatibility note in [`../docs/COMPATIBILITY.md`](../docs/COMPATIBILITY.md).
