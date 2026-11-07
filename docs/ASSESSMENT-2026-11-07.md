# Independent assessment — 2026-11-07

Four role-scoped reviews were run against `v0.16.1`/working tree: application
architecture, lead developer, QA and security architecture. Each cited
`path:line` evidence; findings were re-checked against the code before action.

**Verdict: NO-GO for a pilot with real data as of `v0.16.1`.**
**Conditional GO for a gated canary after the `v0.17.0` hardening below plus
the open blockers.** The decision core, masking math and bundle pipeline are
sound; the gaps were in secure defaults, masking fail-open paths and
deployment/availability controls.

## Remediated in 0.17.0

| Finding | Fix |
|---|---|
| `maskErrors` defaulted false while the contract says true | `Endpoint.MaskErrors` is now `*bool`; nil means true (`config.go`, `engine.go`) |
| Missing mask policy returned the body unmasked | `ErrMaskPolicyMissing`; request fails closed |
| `fieldCallers` bypass by omitting `fields` | Authorization runs over the expanded field set before any upstream call |
| Empty classification map silently masked nothing | `ErrClassificationUnavailable`; fallback fails closed |
| Overlay could delete a locked mask field | Mask-policy overlays merge fields instead of replacing the policy |
| `defaultUnregistered: allow` accepted by the sidecar | Load-time validation rejects it |
| Reload discarded the per-pod PIP cache/breaker | One process-lifetime `pipclient`; only the engine/bundle swaps |
| Duplicate method+path endpoints | Rejected at load |
| PIP-advertised TTL unbounded | `-max-ttl` (default 5m) clamps attribute and classification TTLs |
| PIP had no disruption guard | PDB `minAvailable: 1`, preferred pod anti-affinity |
| CI skipped chart/feature/hack/deploy changes | Path filters fixed; `-race` added |

## Open blockers

| # | Finding | Owner | Gate |
|---|---|---|---|
| C2 | Sidecar trusts client-supplied `x-subject`/`x-claim-*`; no strip/verify anywhere | Platform/gateway + core | Forged-header test must fail |
| C1 | PIP defaults to plaintext with no caller auth; mTLS/caller authz are opt-in | Platform + core | `pip.tls.enabled`, `-client-ca`, `allowedCallers` on every source |
| H | Unsigned bundle mode is the default; `-bundle-pubkey` only warns | Core | Pubkey mandatory in production manifests |
| H | No signed contract version; manifest outside digest | Core | Enforce major in `Validate`; include manifest in digest |
| H | Audit writes stdout synchronously with a global mutex on 100% of denies | Core | Bounded async sink + drop metric |
| H | `fresh`/`deny-on-stale` trust optional lag; no max-TTL before 0.17 | Core | Lag mandatory for those modes; TTL clamp now in |
| H | Shared fail-closed PIP has no dependency-aware readiness | Platform | Readiness fails when upstreams unusable |
| H | Sidecar/Envoy deployment manifest is unowned (service chart absent) | Platform | Reproducible install with pubkey/salt/ext_proc |
| M | No rate limiting (only concurrency cap) | Core/platform | Per-identity limit + 429/503 metric |
| M | DQ has no unit suite; fallback/reload paths lack end-to-end tests | QA | Tests added to CI gates |
| M | Classification tags live outside the signed bundle | Delivery/core | Move into bundle or version/stamp them |
| M | Numeric fidelity loss on masked JSON (float64 re-marshal) | Core | `UseNumber` or raw transform |

## Gate for a canary

1. C1/C2 verified in-cluster (forged header rejected, no-cert PIP call rejected,
   caller outside `allowedCallers` gets 403).
2. Signed bundle enforced; contract version verified.
3. Error-body masking, missing-policy and empty-classification conformance
   cases green.
4. 1,200 RPS/pod against real replicas; reload preserves cache.
5. Security and architecture owners sign this document.

The full QA/security reports remain the source detail; this page is the
decision record.
