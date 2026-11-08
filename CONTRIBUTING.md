# Contributing

## Setup

- Go 1.27+ (the module declares the toolchain), `make`, Docker for the Envoy
  smoke test, Helm 3.16 for chart checks.
- Keep build caches inside the workspace:
  `GOCACHE=$PWD/.gocache GOPATH=$PWD/.gopath`.
- Never use a network proxy for module downloads in this environment; the
  repository builds with `GOPROXY=off` from the module cache.

## Before opening a PR

```bash
make fmt-check
make vet
make test
make acceptance
make steward          # docs, ADR index, links, versions, imports, artifacts
make chart            # Helm lint/render
```

`make smoke` (Docker + Envoy 1.40) is required for changes touching
`sidecar/`, `hack/envoy-smoke.sh` or response masking.

## Change rules

- One logical change per PR; no drive-by formatting.
- New package or top-level directory: add or update an ADR first.
- Version changes must update README, ARCHITECTURE, docs/BDD.md, the Helm
  `appVersion`/image tag and the top CHANGELOG entry together.
- Every new endpoint/rule needs a conformance fixture or BDD scenario; see
  [docs/agents/conformance-steward.md](docs/agents/conformance-steward.md).
- Security-sensitive changes (identity, locked fields, fail modes, signing)
  need security review.
- Do not commit build output (`bin/`, `dist/`), keys, salts or real data.

## Documentation

- Update the README/docs you invalidated, not just the code.
- ADRs are immutable: supersede with a new numbered ADR.
- Run `make steward` before pushing; it fails on broken links, version drift,
  orphan ADRs and import-boundary violations.

## Licensing and ownership

No license or CODEOWNERS file exists yet. Do not add one in a feature PR;
those are maintainer decisions.
