# policy-information-point

The PIP is a **data service** for the sidecar: it resolves resource attributes
from read replicas and serves field classification metadata. It does not make
authorization decisions and must not be used as a PDP.

## API

Normative definition: [`../contracts/pip-api.yaml`](../contracts/pip-api.yaml).

- `POST /v1/attributes:batch` — resolve attributes for up to 100 resources,
  with field projection.
- `GET /v1/classification/{resourceType}` — long-lived classification metadata.
- `GET /version` — `apiVersion` + `build` for the sidecar handshake.
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
      "allowedCallers": ["spiffe://example/ns/app/sa/accounts"],
      "fieldCallers": {
        "ssn": ["spiffe://example/ns/app/sa/pii-reader"]
      },
      "lagHeader": "X-Replica-Lag-Seconds",
      "caFile": "/etc/controls-tls/ca.pem",
      "certFile": "/etc/controls-tls/client.pem",
      "keyFile": "/etc/controls-tls/client-key.pem"
    }
  }
}
```

- `{id}` is path-escaped; the URL host is fixed by configuration (no
  caller-controlled URLs), redirects are refused, and headers expand
  `${ENV}` at load.
- `caFile` / `certFile` / `keyFile` enable server verification and mTLS to the
  attribute API; they require an `https` URL. `serverName` overrides the SNI
  name when the certificate does not match the host.
- `fields` is a hard allowlist: the PIP will never return a field outside it,
  even if the caller asks.
- `allowedCallers` restricts a source to specific client-certificate
  identities (URI SAN preferred, then DNS SAN, then CN). `fieldCallers`
  restricts individual fields further. With mTLS enabled, a caller outside the
  list gets `403`; without mTLS there is no identity, so a non-empty list
  denies everyone (fail closed).
- Upstream responses are capped at 1 MiB; a larger body fails closed.
- `404` means `found: false`; any other non-200 or decode error fails closed.
- `lagHeader` (default `X-Replica-Lag-Seconds`) feeds `freshness`. If the
  upstream omits it, the response is marked `lagKnown: false`, and `fresh` /
  `deny-on-stale` endpoints fail closed with `lag_unknown` instead of treating
  missing lag as zero.
- `healthUrl` is optional. When set, PIP `/readyz` calls it (2s timeout) and
  reports 503 on any non-2xx; leave it unset if the source has no health
  endpoint.
- `versionHeader` (default `X-Resource-Version`, ETag-style quotes stripped)
  feeds the `version` field used for cache invalidation; a missing header
  reports `unknown`.
- The client never uses an environment HTTP proxy, so internal attribute calls
  cannot be routed through an unexpected proxy.

## Reference implementation

`pip/server` ships an in-memory `Repository` backed by
[`fixtures/attributes.json`](fixtures/attributes.json); `pip/adapters/httpapi`
is the production adapter over read APIs in front of the replicas.

Fixtures may carry optional per-resource metadata so a local run can exercise
stale/unknown lag and cache TTL without a custom PIP:

```json
{
  "resources": {"party": {"PTY-1": {"ownerId": "u-1"}}},
  "classifications": {"party": {"ssn": "pii.locked"}},
  "resourceMeta": {
    "party": {
      "PTY-1": {"lagSeconds": 6, "lagKnown": true, "version": "v7", "ttlSeconds": 5}
    }
  }
}
```

`lagKnown` is a pointer: omit it and the PIP reports lag unknown (fresh and
deny-on-stale endpoints fail closed). The process-wide `-lag-seconds` flag still
overrides fixture lag for every resource.

```go
type Repository interface {
    Get(ctx, resourceType, id string, fields []string) (server.Resource, error)
    Classification(ctx, resourceType string) (map[string]string, bool, error)
}
```

### Adapter rules

- Read-only credentials, one grant per domain, views rather than base tables
  where possible.
- Fixed URLs and allowlisted fields only; never interpolate caller input beyond
  the path-escaped `{id}`.
- Always honour the requested field projection.
- Return `found: false` for unknown ids; do not leak existence through errors.
- Bound every query with a timeout and a row limit.
- Report `lagSeconds` from the replica position. The sidecar enforces it for
  `fresh` and `deny-on-stale` registry entries; if the header is missing the
  response is marked `lagKnown: false` and those endpoints fail closed.

## Caching

Two levels, deliberately different TTLs:

| Level | TTL | Invalidation |
|---|---|---|
| Sidecar attributes | 30–60 s | TTL + version bump |
| Sidecar classification | hours | TTL |
| PIP internal | seconds–minutes | change events / replica refresh |

The sidecar coalesces misses; the PIP must survive a cold-start stampede
without saturating the replicas.

## Transport security

- Server TLS: `-tls-cert` / `-tls-key`.
- Mutual TLS: add `-client-ca` to require a verified client certificate.
- Sidecar client: `-pip-ca`, `-pip-cert`, `-pip-key`, `-pip-server-name`
  (`CONTROLS_PIP_*`); these enable HTTPS plus client-certificate
  authentication. `pipclient.TLSConfig` is tested against a real mTLS server.
- The admin listener (`-metrics-addr`) stays plaintext and should be restricted
  to the monitoring network.

## Metrics

`-metrics-addr` (default `:9090`) serves `/metrics`, `/healthz` and `/readyz`:

- `pip_requests_total{endpoint}`
- `pip_request_errors_total{endpoint}`
- `pip_request_duration_seconds{endpoint}` (histogram)
- `pip_attributes_total{resource_type,found}`
- `pip_lag_seconds{resource_type}`
- `pip_rejected_total{reason}` (concurrency saturation)

`-max-ttl` (sidecar flag, default 5m) caps the TTL a PIP can advertise.

`-rate-limit` / `-rate-burst` apply a per-caller token bucket; excess requests
get `429` with `Retry-After`.

`-max-concurrent` bounds in-flight requests; excess requests get `503
{"error":"overloaded"}` instead of queueing behind a slow upstream.

## Failure behaviour

PIP errors cause the sidecar to fail closed. Keep the service small, stateless
and horizontally scalable; 2+ replicas, mTLS, and its own workload identity.
The PIP must never call back through the sidecar.

## Run

```bash
go run ./pip/cmd/pip -runtime-config pip/pip.example.yaml
go run ./pip/cmd/pip -addr :8082 -fixtures pip/fixtures/attributes.json
go run ./pip/cmd/pip -addr :8082 -sources pip-sources.json
```

The YAML file covers listener, metrics, concurrency, TLS, caller checks, rate
limits and the repository (fixtures or source document). Flags override the
file. See [`pip.example.yaml`](pip.example.yaml).
