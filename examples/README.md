# Examples

Self-contained, executable documentation. Each sample owns its app, controls
bundle, Envoy config, compose file and contract test. Samples consume the
sidecar runtime image built from this repository — they never compile the
sidecar themselves.

| Sample | Demonstrates | Status |
|---|---|---|
| [accounts-read](accounts-read/) | route identity, full/partial masking, deny paths | implemented |
| [payments-write](payments-write/) | DQ, rejected fields, write authorization | implemented |
| spring-accounts | Spring Boot app behind the sidecar | planned |
| mtls-caller | PIP mTLS + caller/field authorization | planned |
| pip-outage | fail-closed and stale/lag behavior | planned |

## Adding a sample

1. Create `examples/<name>/` with `app/`, `controls/`, `envoy/`, `pip/`,
   `compose.yaml`, `test.sh`, `README.md`.
2. Use `image: ${CONTROLS_SIDECAR_IMAGE:-controls-sidecar:dev}` and do not
   publish the app port.
3. `test.sh` asserts exact statuses, masked values and deny reasons; it must
   fail when the sidecar contract changes.
4. Add the sample to the CI `examples` matrix and to the table above.

Run one sample:

```bash
make -C examples/accounts-read test
```
