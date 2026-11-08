# Examples

Self-contained, executable documentation. Each sample owns its app, controls
bundle, Envoy config, compose file and contract test. Samples consume the
sidecar runtime image built from this repository — they never compile the
sidecar themselves.

| Sample | Demonstrates | Status |
|---|---|---|
| [accounts-read](accounts-read/) | route identity, full/partial masking, deny paths | implemented |
| [payments-write](payments-write/) | DQ, rejected fields, write authorization | implemented |
| [spring-accounts](spring-accounts/) | Spring Boot entitlement + DQ + OpenAPI annotations | implemented |
| [mtls-caller](mtls-caller/) | PIP mutual TLS + caller authorization | implemented |
| [pip-outage](pip-outage/) | fail-closed when the PIP is unreachable | implemented |

## Adding a sample

1. Create `examples/<name>/` with `app/`, `controls/`, `envoy/`, `pip/`,
   `sidecar.yaml`, `pip/pip.yaml`, `compose.yaml`, `test.sh`, `README.md`.
2. Use `image: ${CONTROLS_SIDECAR_IMAGE:-controls-sidecar:dev}` and do not
   publish the app port.
3. `test.sh` asserts exact statuses, masked values and deny reasons; it must
   fail when the sidecar contract changes.
4. Add the sample to the CI `examples` matrix and to the table above.

`accounts-read` demonstrates the OpenAPI ↔ registry gate: operation annotations
(`x-resource-type`, `x-resource-id`, `x-mask-policy`, `x-resource-source`) are
compared against the registry before the sample runs.

`spring-accounts` goes further: the registry is **generated** from
`openapi.yaml` with `openapi-registry`, and `make test` fails when the committed
registry drifts from the contract.

## Prerequisites

- Docker, Go 1.27+; `spring-accounts` also needs Maven and Java 21.
- The sidecar runtime image is built from this repository, so no registry access
  is required.

Run all samples:

```bash
make examples
```

Each sample starts its own compose project and removes it on success. Use
`make -C examples/<name> up` to keep one running and `down` to stop it.

Run one sample:

```bash
make -C examples/accounts-read test
```
