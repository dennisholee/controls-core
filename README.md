# controls-core

Core engineering monorepo for the **controls plane**: data-level entitlements
and data-quality checks for existing Java/Spring Boot services, enforced by a
centrally managed sidecar with delivery-owned rules and configuration.

Two repositories make up the system:

| Repository | Owner | Contents |
|---|---|---|
| `controls-core` (this repo) | Core engineering | Sidecar engine, PIP, contracts, docs |
| `controls-rules` | Delivery team | Policies, mask policies, registry entries, environment overlays |

```
client ─▶ gateway (authn + coarse authz) ─▶ Envoy sidecar ─▶ Spring Boot service
                                              │
                                              ├─ ext_proc ─▶ registry ─▶ PIP ─▶ read replicas
                                              │              └─ policy eval
                                              └─ response masking + DQ + decision log
```

## Layout

```
sidecar/extproc/    Envoy ext_proc adapter (production transport)
sidecar/envoy/      reference Envoy filter/listener configuration
sidecar/cmd/        extproc server + local HTTP harness
pip/                Policy Information Point (HTTP API over read replicas)
internal/           config, policy, mask, dq, pipclient, engine, openapidiff
contracts/          normative schemas, specs and PIP OpenAPI
features/           BDD acceptance criteria, one file per journey
integration/        end-to-end tests, BDD runner and benchmarks
tools/openapi-diff/ registry ↔ OpenAPI coverage gate
docs/adr/           architecture decisions
```

## Quick start

Requires Go 1.26+.

```bash
make test          # go test ./... (integration tests spin up a fake PIP)
make acceptance    # BDD acceptance criteria against the engine
make bench         # engine latency/memory benchmarks
make pip           # :8082, fixtures in pip/fixtures/attributes.json
make sidecar       # :8081, local HTTP harness
make extproc       # :50051, Envoy ext_proc gRPC service
make smoke         # live Envoy 1.40 in Docker (requires Docker)
```

Exercise the full pipeline:

```bash
curl -s localhost:8081/v1/process -H 'content-type: application/json' -d '{
  "request": {"method":"GET","path":"/accounts/A123",
              "headers":{"x-subject":"u-1","x-claim-region":"US"}},
  "response": {"status":200,"body":{"accountNumber":"1234567890",
              "holder":{"email":"jane.doe@example.com"},"ssn":"111-22-3333"}}
}' | jq
```

Expected: allowed, with `accountNumber` → `******7890`, `holder.email` →
`j***@example.com`, `ssn` → `[REDACTED]`.

`x-subject` and `x-claim-*` are a **local harness convention only**. In
production the gateway verifies the token and sets internal context headers; the
sidecar must strip any client-supplied values.

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — components, flows, failure matrix, latency budget
- [SECURITY.md](SECURITY.md) — threat model and controls
- [TESTING.md](TESTING.md) — test strategy, fixture format, gates
- [docs/BDD.md](docs/BDD.md) — acceptance criteria, BDD model, adoption plan
- [docs/agents/conformance-steward.md](docs/agents/conformance-steward.md) — the acceptance owner role
- [RUNBOOK.md](RUNBOOK.md) — alerts and incident procedures
- [docs/GOVERNANCE.md](docs/GOVERNANCE.md) — ownership, review, break-glass
- [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) — version matrix
- [contracts/](contracts/) — normative registry, masking, PIP API
- [docs/adr/](docs/adr/) — why the design is what it is

## Status

`v0.2.0` — engine, masking, DQ, PIP contract with replica-lag freshness
enforcement, ext_proc adapter (exercised against a live Envoy via `make smoke`)
and OpenAPI coverage gate. Classification-driven fail-safe masking remains
outstanding. See [ARCHITECTURE.md § Known gaps](ARCHITECTURE.md#8-known-gaps).
