# policy-information-point

The PIP is a **data service** for the sidecar: it resolves resource attributes
from read replicas and serves field classification metadata. It does not make
authorization decisions and must not be used as a PDP.

## API

Normative definition: [`../contracts/pip-api.yaml`](../contracts/pip-api.yaml).

- `POST /v1/attributes:batch` — resolve attributes for up to 100 resources,
  with field projection.
- `GET /v1/classification/{resourceType}` — long-lived classification metadata.
- `GET /healthz`.

## Attribute sources

Two repositories implement the data port:

1. **Fixtures** (default) — `-fixtures`, for local development and smoke tests.
2. **HTTP attribute APIs** — `-sources`, for production. Each resource type maps
   to a read API in front of the replicas:

```json
{
  "sources": {
    "account": {
      "url": "https://accounts.internal/accounts/{id}",
      "timeoutMs": 100,
      "headers": {"Authorization": "Bearer ${ACCOUNTS_TOKEN}"},
      "fields": ["ownerId", "region", "status"],
      "classification": {
        "accountNumber": "pii.partial",
        "ssn": "pii.locked"
      },
      "lagHeader": "X-Replica-Lag-Seconds"
    }
  }
}
```

- `{id}` is path-escaped; the URL host is fixed by configuration (no
  caller-controlled URLs), redirects are refused, and headers expand
  `${ENV}` at load.
- `fields` is a hard allowlist: the PIP will never return a field outside it,
  even if the caller asks.
- `404` means `found: false`; any other non-200 or decode error fails closed.
- `lagHeader` (default `X-Replica-Lag-Seconds`) feeds `freshness`; if the
  upstream omits it, lag is zero.

## Reference implementation

`pip/server` ships an in-memory `Repository` backed by
[`fixtures/attributes.json`](fixtures/attributes.json). The production
implementation registers per-domain adapters that query read replicas with
curated, parameterized statements.

```go
type Repository interface {
    Get(ctx, resourceType, id string, fields []string) (map[string]any, bool, error)
    Classification(ctx, resourceType string) (map[string]string, bool, error)
}
```

### Adapter rules

- Read-only credentials, one grant per domain, views rather than base tables
  where possible.
- Fixed queries only. Never translate client input into SQL or JSONPath-to-SQL.
- Always honour the requested field projection.
- Return `found: false` for unknown ids; do not leak existence through errors.
- Bound every query with a timeout and a row limit.
- Report `lagSeconds` from the replica position. The sidecar enforces it for
  `fresh` and `deny-on-stale` registry entries; a missing value is treated as
  zero and therefore passes.

## Caching

Two levels, deliberately different TTLs:

| Level | TTL | Invalidation |
|---|---|---|
| Sidecar attributes | 30–60 s | TTL + version bump |
| Sidecar classification | hours | TTL |
| PIP internal | seconds–minutes | change events / replica refresh |

The sidecar coalesces misses; the PIP must survive a cold-start stampede
without saturating the replicas.

## Failure behaviour

PIP errors cause the sidecar to fail closed. Keep the service small, stateless
and horizontally scalable; 2+ replicas, mTLS, and its own workload identity.
The PIP must never call back through the sidecar.

## Run

```bash
go run ./pip/cmd/pip -addr :8082 -fixtures pip/fixtures/attributes.json
go run ./pip/cmd/pip -addr :8082 -sources pip-sources.json
```
