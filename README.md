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
hack/               smoke stack, fixture app and load generator
tools/bundle/       deterministic bundle renderer (Jenkins entry point)
tools/conformance/  executes fixtures against a rendered bundle
tools/openapi-diff/ registry ↔ OpenAPI coverage gate
ci/Jenkinsfile      reference pipeline integration
deploy/             tools/PIP/extproc images and the Helm chart
deploy/helm/controls-plane/  PIP, NetworkPolicies, Istio mTLS/authorization
docs/adr/           architecture decisions
```

## Quick start

Requires Go 1.26+.

```bash
make test          # go test ./... (integration tests spin up a fake PIP)
make testdrive     # one-command local check; no Docker needed
make demo          # keep the Envoy + smokeapp stack running (Docker); :18000 is controlled, :18080 is the unmasked upstream
make acceptance    # BDD acceptance criteria against the engine
make bench         # engine latency/memory benchmarks
make api           # :8081 test API + in-process PIP, console at /docs
make pip           # :8082, fixtures in pip/fixtures/attributes.json
make sidecar       # :8081, harness against an external PIP
make extproc       # :50051, Envoy ext_proc gRPC service
make smoke         # live Envoy 1.40 in Docker (requires Docker)
```

`extproc` also serves `/metrics`, `/healthz` and `/readyz` on `:9090`; audit
lines go to stdout. See [RUNBOOK.md](RUNBOOK.md#1a-metrics-mapping).

Render and check a delivery bundle:

```bash
make build
./bin/bundle -src ../controls-rules -env prod -service accounts -out dist/accounts
./bin/conformance -bundle dist/accounts -fixtures ../controls-rules/tests/fixtures -service accounts -v
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

`make api` runs the same engine as a single process with fixture-backed
attributes. Open `http://127.0.0.1:8081/docs` for a browser console, or import
`http://127.0.0.1:8081/openapi.yaml` into Postman. `POST /v1/test` accepts a
conformance fixture and returns pass/fail checks, and
`make api-rules ENV=dev` renders and serves the delivery-owned rules from
`../controls-rules`. See [docs/TEST-API.md](docs/TEST-API.md) for the full
contract, examples and limitations.

## Documentation

- [CONTRIBUTING.md](CONTRIBUTING.md) — setup, PR gates, documentation and review rules
- [ARCHITECTURE.md](ARCHITECTURE.md) — components, flows, failure matrix, latency budget
- [SECURITY.md](SECURITY.md) — threat model and controls
- [TESTING.md](TESTING.md) — test strategy, fixture format, gates
- [docs/TEST-API.md](docs/TEST-API.md) — exercise the solution through the HTTP test API
- [docs/BDD.md](docs/BDD.md) — acceptance criteria, BDD model, adoption plan
- [docs/agents/conformance-steward.md](docs/agents/conformance-steward.md) — the acceptance owner role
- [RUNBOOK.md](RUNBOOK.md) — alerts and incident procedures
- [docs/GOVERNANCE.md](docs/GOVERNANCE.md) — ownership, review, break-glass
- [deploy/helm/controls-sidecar/README.md](deploy/helm/controls-sidecar/README.md) — sidecar injection library chart
- [docs/REPO-GOVERNANCE.md](docs/REPO-GOVERNANCE.md) — layout, boundaries, docs and CI rules
- [docs/agents/repo-steward.md](docs/agents/repo-steward.md) — repository steward charter (`make steward`)
- [docs/IDENTITY.md](docs/IDENTITY.md) — gateway identity envelope and header-trust model
- [docs/ASSESSMENT-2026-11-07.md](docs/ASSESSMENT-2026-11-07.md) — independent assessment and remediation status
- [docs/PILOT-CHECKLIST.md](docs/PILOT-CHECKLIST.md) — cluster prerequisite/verification checklist
- [docs/PIPELINE.md](docs/PIPELINE.md) — Jenkins contract: inputs, stages, outputs
- [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) — version matrix
- [contracts/](contracts/) — normative registry, masking, PIP API
- [docs/adr/](docs/adr/) — why the design is what it is

## Status

`v0.38.2` — engine, masking with classification fallback, DQ, replica-lag
freshness, ext_proc adapter (exercised against a live Envoy via `make smoke`),
the pipeline entry points (`bundle`, `bundle-sign`, `conformance`, OpenAPI
gate), audited/metered enforcement, verified bundle integrity, deployment
controls (Helm: PIP isolation, NetworkPolicies, mTLS), HTTP attribute sources
for the PIP, an optional Cedar policy engine, external OPA decisions and signed
bundle reload. Remaining: cluster verification, gateway signer deployment and
digest-pinned base images. See
[ARCHITECTURE.md § Known gaps](ARCHITECTURE.md#8-known-gaps).
