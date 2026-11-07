# Independent assessment — 2026-11-07

Four role-scoped reviews were run against `v0.16.1`/working tree: application
architecture, lead developer, QA and security architecture. Each cited
`path:line` evidence; findings were re-checked against the code before action.

**Verdict: NO-GO for a pilot with real data as of `v0.16.1`.**
**Conditional GO for a gated canary after the `v0.17.0` hardening below plus
the open blockers.** The decision core, masking math and bundle pipeline are
sound; the gaps were in secure defaults, masking fail-open paths and
deployment/availability controls.

The lead-developer review added two critical availability defects (breaker
never recovering, unbounded cache) and hot-path correctness issues (query
strings, identity normalization, per-stream engine consistency). Those are
fixed in 0.18.0.

## Remediated in 0.17.0–0.18.0

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
| PIP breaker never half-opened; rejections kept extending the window | Half-open single-probe state; rejections no longer count as failures |
| Attribute cache unbounded and cached not-found results | 100k-entry cap with eviction; not-found results are not cached |
| Query strings corrupted path resource IDs | Query stripped before matching |
| Encoded/traversal IDs and numeric body IDs | Path IDs decoded and validated; body decoded with `UseNumber` |
| Policy-init failures bypassed audit/metrics | Hook defer installed before the init-error return |
| Decision and masking could use different bundles | Engine pinned per ext_proc stream |
| DQ regex recompiled per request; unknown ops/types were silent | Rules compiled and validated at load; DQ unit tests added |

## Open blockers

| # | Finding | Owner | Gate |
|---|---|---|---|
| C2 | Sidecar trusts raw `x-subject`/`x-claim-*` unless `CONTROLS_IDENTITY_HMAC_KEY` is set; gateway signing not yet deployed | Platform/gateway + core | Envelope enforced; forged raw header test fails |
| C1 | PIP defaults to plaintext; `-require-mtls` / `-require-caller` are opt-in | Platform + core | `pip.tls.required`, `allowedCallers` on every source |
| H | Unsigned bundle mode now requires explicit `-allow-unsigned` | Core | Remove dev escape from production manifests |
| H | No signed contract version; manifest outside digest | Core | Enforce major in `Validate`; include manifest in digest |
| H | Audit writes stdout synchronously with a global mutex on 100% of denies | Core | Bounded async sink + drop metric |
| H | `fresh`/`deny-on-stale` trust optional lag; no max-TTL before 0.17 | Core | Lag mandatory for those modes; TTL clamp now in |
| H | Shared fail-closed PIP has no dependency-aware readiness | Platform | Readiness fails when upstreams unusable |
| H | Sidecar/Envoy deployment manifest is unowned (service chart absent) | Platform | Reproducible install with pubkey/salt/ext_proc |
| M | No rate limiting (only concurrency cap) | Core/platform | Per-identity limit + 429/503 metric |
| M | DQ has no unit suite; fallback/reload paths lack end-to-end tests | QA | Tests added to CI gates |
| M | Classification tags live outside the signed bundle | Delivery/core | Move into bundle or version/stamp them |
| — | `rejectFields` wildcard support added | Core | Done in 0.19.0 |
| M | Audit writes fail silently and share stdout with logs | Core | Dedicated sink + failure counter |
| M | Rule comparisons coerce strings, enabling body-sourced bypass | Core | Strict typed comparison |
| M | Base images and Jenkins tools image are tag/placeholder pinned | Core | Digest pinning |

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
