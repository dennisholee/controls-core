# ADR-0011 — Bundle reload by polling plus SIGHUP

Status: accepted (2026-11-07)

## Context

Kubernetes ConfigMap updates do not signal the pods that mount them, and the
sidecar must not continue serving a policy set that has been superseded. A
full pod restart works at five pods, but it is disruptive and prevents quick
rollback-free policy fixes.

## Decision

- Poll the bundle directory every 30 seconds (configurable), comparing a
  SHA-256 fingerprint of the bundle files.
- `SIGHUP` forces a reload regardless of the fingerprint.
- On every reload: re-verify the signature, re-parse the policy engine, rebuild
  the audit/metrics hooks, and atomically swap the engine pointer used by the
  gRPC adapter.
- Any failure keeps the previous bundle and logs the error; polling retries
  once the directory changes again.
- `controls_bundle_info` is reset on swap so only the current bundle is
  reported.

## Consequences

- Policy changes take effect within the poll interval without a restart.
- A partially written ConfigMap could be observed; the strict loader and
  signature verification reject it and retain the previous bundle.
- Stale metrics label sets are dropped, but per-decision counters remain
  cumulative.
- Filesystem watch APIs were rejected: polling is portable, cheap at this
  bundle size, and easy to test.

## Alternatives

- fsnotify: lower latency, more platform-specific failure modes.
- Restart-on-change: simpler but disrupts traffic and loses in-process caches.
