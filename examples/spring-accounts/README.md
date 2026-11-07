# Sample: Spring Boot entitlement, masking and DQ

A Spring Boot service behind the sidecar. The application owns the routes; the
sidecar authorizes account reads and payment writes, masks responses, and
enforces payment DQ and rejected fields.

```
client -> sidecar (Envoy + extproc) :18005 -> Spring Boot :8080
                                  \-> PIP :8082
```

## Standard Maven build

`mvn verify` needs no Go, Node or Docker. `RegistryContractTest` parses
`openapi.yaml` and fails if `controls/registry.json` disagrees with the
operation annotations.

`make registry` is the generation step:

```bash
make registry   # openapi.yaml -> controls/registry.json
```

The central pipeline owns generation/validation; the sample commits the
generated registry for review.

## Run

```bash
make test    # drift check, annotation gate, compose contract tests
make up      # keep it running
make down
```

Requires Docker, Go 1.26+ and Maven (offline build uses the local repository).

```bash
curl -s http://127.0.0.1:18005/accounts/A123 -H 'x-subject: u-1'
```

## OpenAPI contract

`openapi.yaml` is a full OpenAPI 3.0.3 document. Operations carry
`x-resource-type`, `x-resource-id`, `x-mask-policy`, `x-resource-source` and
`x-attributes`; these are the source for the generated registry.

## What it proves

- route-bound identity and PIP lookup for reads and writes;
- full and partial masking, including preserved email domain;
- owner-only payment writes;
- blocking DQ and PAN rejection before the upstream;
- OpenAPI annotations generate the sidecar registry, with drift caught by
  `mvn verify`.

`test.sh` fails if any of those behaviours changes.
