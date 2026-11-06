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
```
