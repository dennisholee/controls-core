# Agent guide — controls-core

Read this before changing code. This repository is the canonical engine and
PIP; the service demo is a separate repository.

## Start here

- [`README.md`](README.md) — build, test and deployment entry points.
- [`ARCHITECTURE.md`](ARCHITECTURE.md) — components and request flow.
- [`docs/SIDECAR-GUIDE.md`](docs/SIDECAR-GUIDE.md) — adopter contract.
- [`docs/DEPRECATION.md`](docs/DEPRECATION.md) — removal schedule.
- [`PLATFORM.md`](PLATFORM.md) — three-repository map.

## Commands

```bash
make test            # Go unit/integration
make smoke           # real Envoy + ext_proc path
make acceptance      # BDD scenarios
make openapi-gate    # generated registries match contracts
make eol-check       # EOL bases and deprecated Envoy fields
make examples-config-check
```

## Guardrails

- Do not hand-edit generated registries or `target/` output; edit OpenAPI
  annotations or the generator and regenerate.
- Keep the sidecar fail-closed for registered routes.
- Do not enable `allowUnsigned` or raw identity headers in production paths.
- Runtime base images are digest-pinned; keep `make eol-check` green.
- Go 1.27 is required.
- The workspace-level root `AGENTS.md` is a generated map of all local
  repositories; this file is the repository-specific guide committed to GitHub.
