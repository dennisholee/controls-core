# Repository governance

The repository steward enforces the mechanical parts in CI; humans own the
judgment calls. Run `make steward` locally before a large change.

## Layout contract

| Area | Rule |
|---|---|
| `internal/` | domain and adapters; no transport or process wiring |
| `pip/` | PIP server, sources and command; must not import `sidecar/` |
| `sidecar/` | Envoy adapter and commands; may import `internal/` |
| `contracts/` | normative schemas/specs; no Go code except embedding |
| `deploy/` | images and Helm; no application source |
| `tools/` | one binary per directory; no shared mutable state |
| `docs/` | decisions and operations; ADRs indexed in `docs/adr/README.md` |

## Boundary rules

- `internal/engine` must not import `sidecar/` or `pip/`.
- `internal/mask`, `internal/dq`, `internal/policy` must not import transport.
- `pip/server` must not import `sidecar/`.
- `internal/pipclient` is the only PIP data port used by the engine.

## Documentation contract

- Required: `README.md`, `ARCHITECTURE.md`, `SECURITY.md`, `TESTING.md`,
  `RUNBOOK.md`, `CHANGELOG.md`, `docs/agents/README.md`.
- Every ADR file is linked from `docs/adr/README.md`.
- Relative links in Markdown must resolve.
- Recommended but not yet present: `CONTRIBUTING.md`, `CODEOWNERS`, `LICENSE`.
  These are human decisions; the steward only flags them.

## Versioning contract

`README.md`, `ARCHITECTURE.md`, `docs/BDD.md`, the Helm `appVersion`/image tag
and the top `CHANGELOG.md` entry must name the same version. CI fails on drift.

## CI gates

- `make fmt-check`, `make vet`, `make test`, `-race`, acceptance, build,
  OpenAPI gate.
- `make steward` — docs, ADR index, links, versions, imports, tracked artifacts.
- Helm lint/render and the live Envoy smoke.

## Ownership

Until `CODEOWNERS` exists: core engineering owns `internal/`, `sidecar/`,
`pip/`, `contracts/`, `deploy/`; delivery owns `controls-rules`; security owns
the sign-off for locked fields, fail modes and identity.
