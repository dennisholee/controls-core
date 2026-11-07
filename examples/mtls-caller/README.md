# Sample: mutual TLS to the PIP

The PIP requires a client certificate; the sidecar presents one and verifies
the PIP server against the sample CA.

```bash
make test
```

Certificates are generated on demand under `certs/` and are test-only. The PIP
server SAN is `pip` (the compose service name).

This sample demonstrates transport identity. Per-caller/field authorization
requires the HTTP attribute-source adapter (`allowedCallers`/`fieldCallers`).
