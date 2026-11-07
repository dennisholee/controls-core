# Security

Scope: the controls sidecar, the PIP, the bundle/config pipeline and the
decision-log path.

## 1. Assets

- The **decision** (allow/deny) for data-level entitlements.
- **Resource attributes** and **classification metadata** served by the PIP.
- **Unmasked payloads** traversing the sidecar and application.
- **Bundle integrity** (policy, registry, mask and DQ configuration).
- **Audit records** of decisions and violations.

## 2. Threat model

| ID | Threat | Vector | Control | Residual |
|---|---|---|---|---|
| T1 | Caller selects its own policy | Client sends `_type`/`_id` that the sidecar trusts | Type and identity bound to the route; payload cross-checked; mismatch ⇒ 403 + metric | None if registry is correct |
| T2 | Unconfigured route bypasses controls | New endpoint ships without registry entry | `defaultUnregistered: deny`; OpenAPI diff gate; route-coverage test | Gate must actually run |
| T3 | Identity spoofing | Client supplies `x-subject` / claim headers | Gateway strips client values and injects verified ones; sidecar accepts only from trusted gateway/sidecar chain | Depends on gateway config |
| T4 | PIP becomes a data-exfiltration API | Caller-influenced queries, over-broad projections | mTLS workload identity; fixed, parameterized queries per domain; field projection; no ad-hoc SQL; per-domain read grants; rate limits | Query review discipline |
| T5 | PII in logs | Payload or attributes logged | Decision log stores hashes and rule ids only; no payloads; sampled allows | App logs remain out of scope |
| T6 | Bundle tampering | Modified policy/config in transit or registry | Signed bundle, digest pinning, least-privilege registry, review | Key management |
| T7 | Stale or poisoned cache | Cross-tenant key collisions, long TTL | Tenant-scoped cache keys; TTL; invalidation on version bump; fail closed | Replica lag window |
| T8 | Sensitive error bodies | Exceptions echo PII | `maskErrors: true` by default | Non-JSON errors |
| T9 | Bypass via non-JSON / streaming / compression | Content negotiation tricks | JSON-only masking; strip `Accept-Encoding`; SSE routes explicitly excluded and audited | Explicit bypass list |
| T10 | Over-privileged read replicas | PIP connection can write or read unrelated schemas | Read-only credentials, per-domain views/grants | DBA review |
| T11 | Supply chain | Malicious dependency or image | Stdlib-only reference; SBOM and signed images in CI; pinned digests | Build platform |
| T12 | Mask relaxation | Journey config sets locked field to `none` | Locked fields enforced at validation (code + pipeline) | Review bypass |

## 3. Trust boundaries

```
client ── untrusted ──▶ gateway ── verified identity ──▶ sidecar ── mTLS ──▶ PIP ──▶ replicas
                                      │
                                      └── JSON response masking ──▶ client
```

- The sidecar **never** accepts identity from the request body or from an
  untrusted header. It relies on the gateway-sidecar trust chain.
- The PIP **never** calls back through the sidecar. It has its own workload
  identity and its own database credentials.
- Service-to-service traffic is authenticated with mTLS; the PIP API is not
  exposed outside the cluster.

## 4. Data handling

- **Never logged**: request/response bodies, attribute values, raw subject or
  resource identifiers.
- **Audit hashing**: identifiers are SHA-256 hashes with a per-environment salt
  (`CONTROLS_AUDIT_SALT`). The salt is a secret; rotating it breaks correlation
  with historical logs and must be coordinated with the audit owner.
- **Logged**: endpoint id, hashed subject/resource, decision, reason, policy
  version, bundle digest, latency, DQ rule ids.
- **Retention**: decision logs 90 days by default; audit export per compliance
  requirement.
- Masking happens at the sidecar, so unmasked PII is present in application
  memory and potentially application logs. This was explicitly accepted for
  subject-journey scope; see [ADR-0009](docs/adr/0009-masking-at-edge.md).

## 5. Security review triggers

Require security review for changes to: locked fields, `default` masking mode,
`defaultUnregistered`, trust in identity headers, PIP projection rules, bundle
signing/verification, and any new non-JSON bypass.

## 6. Audit

The latest review is [docs/SECURITY-AUDIT.md](docs/SECURITY-AUDIT.md). Open
release blockers: unauthenticated PIP access (C1) and unverified identity
headers at the sidecar (C2). Bundle signature verification (H1) is also open.
Deployments must not expose the PIP or the sidecar listener outside the trusted
network until those are resolved. Bundle signature verification is implemented
but only active when `-bundle-pubkey` (or `CONTROLS_BUNDLE_PUBKEY`) is set;
production must set it.

## 7. Reporting

Report suspected bypasses or data exposure through the internal security
channel. Do not open a public issue.
