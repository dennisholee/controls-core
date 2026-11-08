# Sample: PIP outage fails closed

The PIP is deliberately unreachable. The sidecar must return `503
pip_unavailable` and must not call the upstream.

`freshness: deny-on-stale` makes the dependency explicit: without a
source-reported lag the sidecar refuses to serve cached attributes as current.

```bash
make test
```

This sample has no PIP service; `sidecar.yaml` points `pip.url` at a closed port.
