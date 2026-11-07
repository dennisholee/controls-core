# Sample: Spring Boot account read

A Spring Boot service behind the sidecar. The application owns the routes; the
sidecar authorizes account reads, masks the response, and enforces payment DQ
and rejected fields on writes. `openapi.yaml` carries the registry-binding
annotations and is checked by `make test`.

Smallest end-to-end sample:

```
client -> sidecar (Envoy + extproc) :18000 -> app :8080
                                  \-> PIP :8082
```

The app returns raw account data. The sidecar authorizes the read and masks
`accountNumber`, `holder.email` and `ssn` on the response. The app is not
published directly.

## Run

```bash
make test    # builds images, starts compose, runs contract assertions
make up      # keep it running
make down
```

Requires Docker, Go 1.26+ and Maven (offline build uses the local repository).

```bash
curl -s http://127.0.0.1:18000/accounts/A123 -H 'x-subject: u-1'
```

## What it proves

- route-bound identity and PIP lookup;
- full and partial masking, including preserved email domain;
- deny for another subject and for missing identity;
- the upstream is reachable only through the sidecar.

`test.sh` fails if any of those behaviours changes.
