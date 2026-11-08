# Security audit — controls workspace

Date: 2026-11-07
Scope: `controls-core` (sidecar, PIP, tools, pipeline contract) and
`controls-rules` (rules, registry, overlays, fixtures).
Method: manual code review, dependency manifest review, secret grep, git
history scan, injection-surface review. `govulncheck` could not run (module
proxy unreachable from this environment); see M7.

## Summary

| Severity | Count |
|---|---|
| CRITICAL | 2 (controls provided, install/verify required) |
| HIGH | 3 (all remediated) |
| MEDIUM | 8 |
| LOW | 2 |

The architecture is sound on the decision path (route-bound identity, deny by
default, deny-first policy, fail-closed PIP, locked fields). The critical and
high findings are **deployment and supply-chain** issues around the reference
implementations, not the policy engine itself.

## CRITICAL

### C1 — The PIP exposes PII attributes without authentication
Evidence: [`pip/server/server.go:49`](../pip/server/server.go#L49) decodes and
serves every batch request with no authentication or authorization;
[`pip/cmd/pip/main.go:21`](../pip/cmd/pip/main.go#L21) defaults to `:8082` (all
interfaces); field projection is caller-controlled, so `ssn` and any other
attribute can be requested for a known id; the classification endpoint is also
open.
Impact: anyone who can reach the PIP can enumerate customer attributes. The
sidecar's mTLS expectation is documentation, not code.
Remediation: terminate mTLS in the PIP and verify the caller workload identity;
add a network policy that only admits sidecars; restrict the projection to an
allowlist per caller; bind loopback in local runs; consider moving the PIP
behind the service mesh with authorization policy. Treat as a release blocker
for any environment containing real data.

Status: **deployment controls provided.** The Helm chart applies a
NetworkPolicy that admits only pods labeled `controls-plane/sidecar=true`, and
optional Istio `PeerAuthentication: STRICT` plus an `AuthorizationPolicy`
limited to listed service-account principals. Application-level mutual TLS is
also available (`pip -tls-cert/-tls-key/-client-ca` and sidecar
`-pip-ca/-pip-cert/-pip-key`). The finding remains open until one of these is
installed and verified. Per-caller field authorization is now available
through `allowedCallers` / `fieldCallers` using the mTLS identity.

### C2 — Identity headers are trusted without verification at the sidecar
Evidence: [`sidecar/extproc/adapter.go:231`](../sidecar/extproc/adapter.go#L231)
reads `x-subject` and `x-claim-*` directly from the request;
[`sidecar/envoy/static-sidecar.yaml:14`](../sidecar/envoy/static-sidecar.yaml#L14)
binds `0.0.0.0:8080` with no TLS or authentication; the ext_proc gRPC listener is
plaintext ([`sidecar/cmd/extproc/main.go:88`](../sidecar/cmd/extproc/main.go#L88)).
Impact: if the sidecar listener is reachable other than through a gateway that
strips and re-injects these headers, a caller can impersonate any subject and
satisfy region/owner rules. This is an authorization bypass.
Remediation: network policy and ingress such that only the gateway reaches the
sidecar; gateway must strip all `x-subject`/`x-claim-*` from client requests;
add mTLS/SPIFFE between gateway and sidecar and require a signed or
gateway-injected trust marker; never expose the static listener directly. Add a
smoke case asserting that a client-supplied identity header is replaced.

Status: **deployment controls provided.** The Helm chart's sidecar
NetworkPolicy admits the app port only from the configured gateway
namespace/pod selector; the gateway remains responsible for stripping and
re-injecting identity headers.

## HIGH

### H1 — Bundle signatures are described but never verified
Evidence: [`config.go:168`](../internal/config/config.go#L168) loads
`manifest.json` for metadata only; there is no signature verification anywhere
in the sidecar, while [`docs/PIPELINE.md:120`](PIPELINE.md#L120) says the
pipeline signs the bundle.
Impact: a modified ConfigMap or artifact changes policy and masking while the
audit log still shows the original digest, so provenance is not proven.
Remediation: verify a signature (or an admission-control policy) over the
bundle digest at startup and on reload; fail closed if verification fails; keep
the public key outside the bundle.

Status: **remediated.** `bundle-sign` produces an Ed25519 detached signature
over the recomputed digest; the sidecar takes `-bundle-pubkey` /
`CONTROLS_BUNDLE_PUBKEY` and refuses to start when verification fails.
Tampering with any bundle file now causes a digest mismatch before the
signature check, and both paths are tested.

### H2 — `defaultUnregistered: allow` can be introduced through an overlay
Evidence: [`bundle.go:337`](../internal/bundle/bundle.go#L337) copies the overlay
value unchanged; [`config.go:197`](../internal/config/config.go#L197) accepts
`allow`; the rules validator only checks registry files and the top-level env
field ([`validate.py:108`](https://github.com/dennisholee/controls-rules/tools/validate.py#L108)), not
`bundle.defaultUnregistered` inside an overlay.
Impact: a production overlay can turn off fail-closed handling of unregistered
routes, which is the primary route-coverage control.
Remediation: the renderer should reject `allow` (or require an explicit
`-allow-unregistered` flag plus a non-production environment); extend the
validator to inspect overlay contents.

Status: **remediated during this audit.** The renderer rejects
`defaultUnregistered: allow` in overlays and the test suite covers it. The rules
validator also inspects overlay contents.

### H3 — The PIP accepts unbounded batches and bodies
Evidence: [`pip/server/server.go:49`](../pip/server/server.go#L49) decodes the
request body without `http.MaxBytesReader`; the OpenAPI limit of 100 refs is not
enforced; there is no rate limiting.
Impact: a single caller can exhaust PIP memory/CPU; because entitlement fails
closed, that becomes a fleet-wide outage.
Remediation: 64 KiB body limit, batch cap of 100, per-caller rate limiting,
query timeouts, and a concurrency limit on the PIP.

Status: **partially remediated.** The 64 KiB body limit and 100-ref batch cap
are implemented and tested; rate limiting and query concurrency limits remain
with the platform.

## MEDIUM

### M1 — Unknown overlay keys are silently ignored
[`bundle.go:334`](../internal/bundle/bundle.go#L334) uses plain `json.Unmarshal`;
a typo such as `disableRule` is accepted and the intended control stays enabled.
The docs claim unknown keys are rejected. Use `DisallowUnknownFields` for
overlays and bundle documents.

Status: **remediated.** Overlays are decoded with `DisallowUnknownFields` and
an unknown key fails the render.

### M2 — Tools container runs as root
[`Dockerfile.tools:16`](../deploy/Dockerfile.tools#L16) had no `USER`, and the
Jenkinsfile ran the agent with `-u root`
([`Jenkinsfile:10`](../ci/Jenkinsfile#L10)).

Status: **remediated.** The tools image creates and runs as UID 10001, and the
Jenkins agent runs as the workspace UID (`--user 1000:1000`) instead of root.

### M3 — Base images are tag-pinned, not digest-pinned
`golang:1.26`, `alpine:3.20` ([`Dockerfile.tools:7`](../deploy/Dockerfile.tools#L7))
and the smoke Envoy tag are mutable. Pin by digest in the image build and CI.

Status: **partially remediated.** The smoke test now pins the Envoy digest. The
Go and Alpine base digests could not be resolved from this environment (Docker
Hub unreachable); pin them in CI where the registry is available.

### M4 — Non-JSON responses bypass masking
[`engine.go:287`](../internal/engine/engine.go#L287) returned non-JSON bodies
unchanged. Any sensitive endpoint that can emit non-JSON had to be blocked at
the gateway; this was documented but not enforced by the registry.

Status: **remediated.** A masked endpoint now returns an error (fail closed)
for a non-JSON body unless it explicitly sets `allowNonJSON: true`. The option
is part of the registry contract and covered by a test.

### M5 — No TLS or authentication on the Envoy listener or ext_proc gRPC
The standalone reference listens on `0.0.0.0:8080` and the adapter on
`:50051`. mTLS is expected from the platform but absent from the reference
config. Enforce at the mesh/network layer and document it as a prerequisite.

### M6 — No rate limiting on control endpoints
Neither the PIP nor ext_proc limits request rates; a noisy tenant can degrade
the shared controls plane. Add local or gateway rate limits per identity.

### M7 — Dependency advisory scan not executed
`govulncheck` could not reach `proxy.golang.org` from this environment. The
direct dependencies are recent (`envoy v1.40.0`, `grpc v1.85.0`,
`protobuf v1.36.12`), but this is not a clean advisory result.

Status: **CI gate added.** `make vulncheck` runs
`govulncheck@v1.1.4 ./...`, and the GitHub Actions test job enforces it. The
first CI run will establish the clean-or-not result.

### M8 — Audit salt can silently fall back to a known value
[`extproc/main.go:49`](../sidecar/cmd/extproc/main.go#L49) warns and uses
`local-dev-salt` when `CONTROLS_AUDIT_SALT` is unset, and the salt can be passed
as a CLI flag visible in process listings. A predictable salt allows dictionary
re-identification of hashed subject ids. Fail startup in production and accept
the salt only from a secret-backed environment variable.

Status: **remediated.** The ext_proc service refuses to start without
`CONTROLS_AUDIT_SALT`; local targets set an explicit development salt.

## LOW

### L1 — Client-controlled request id in audit correlation
`x-request-id` is copied into the decision log. A caller can spoof correlation
ids; use a gateway-generated id or record both.

### L2 — Metrics endpoint is unauthenticated
`:9090/metrics` exposes endpoint names, reasons and bundle digest. Restrict it
to the monitoring network or add auth at the scrape proxy.

## Checks passed

- No secrets in tracked files, `.env` files, or git history (`git log -S` for
  password/private-key/AWS patterns returned nothing).
- No shell execution, `eval`, SQL construction, or template injection surfaces;
  no `os/exec` and no `database/sql` in the module.
- Route matching is exact and default-deny; unregistered routes are rejected.
- Payload `_type`/`_id` cannot select policy; the registry is authoritative.
- Deny rules are absolute; `defaultEffect` is deny.
- Locked mask fields cannot be relaxed by configuration; validated in code.
- PIP failures and stale attributes fail closed.
- ext_proc caps buffered bodies at 1 MiB.
- Audit logs contain hashed identifiers only; metric labels are escaped.
- `go vet` and the full test/acceptance suite pass.

## Remediated during the audit

H1 (bundle signatures), H2 (fail-open overlay), H3 (PIP limits, partially),
M1 (unknown overlay keys) and M8 (audit salt) were fixed and covered by tests.

## Release gate

C1 and C2 must be resolved (or formally accepted with compensating network
controls) before any production data is in scope. H1–H3 are remediated; M1–M3
are quick hardening items suitable for the same change window.
