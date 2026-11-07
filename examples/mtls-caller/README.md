# Sample: mutual TLS to the PIP

The PIP requires a client certificate; the sidecar presents one and verifies
the PIP server against the sample CA.

```bash
make test
```

Certificates are generated on demand under `certs/` and are test-only. The PIP
server SAN is `pip` (the compose service name).

The PIP resolves attributes through an HTTP source and restricts that source to
the client certificate identity `sidecar` via `allowedCallers`. The sample
therefore demonstrates both transport identity and caller authorization.
