# Pilot checklist

Use this before pointing real traffic at the controls plane. Everything is
implemented and tested locally; the items marked **[env]** require the target
cluster or registry and could not be validated in the build environment.

## 1. Build and publish

- [ ] Build `controls-pip`, `controls-extproc` and `controls-tools` images.
- [ ] Pin every base image by digest (`golang`, `alpine`) and sign the images.
- [ ] Publish `controls-tools` and record its digest; pin it in Jenkins.
- [ ] Generate the bundle signing key (`bundle-keygen`), store the private key
      in Jenkins credentials, publish the public key to the cluster.

## 2. Cluster prerequisites **[env]**

- [ ] CNI enforces NetworkPolicy (Calico, Cilium, Antrea…). Verify with
      `kubectl describe networkpolicy`.
- [ ] Namespace labels match the chart values (`networkPolicy.*` selectors).
- [ ] Gateway is the only route to the sidecar; it strips client-supplied
      `x-subject`/`x-claim-*`.
- [ ] If using Istio: strict mTLS and the PIP `AuthorizationPolicy` principals.
- [ ] If using application mTLS: PIP server cert/key/client-CA secret and the
      sidecar client cert material (`sidecar.pipTLS`).
- [ ] SPIFFE identities exist for sidecars and are listed in
      `allowedCallers`/`fieldCallers` where required.

## 3. Configure

- [ ] `CONTROLS_AUDIT_SALT` from a secret (not a flag/process list).
- [ ] `CONTROLS_BUNDLE_PUBKEY` mounted read-only.
- [ ] PIP `-sources` document with real read APIs, field allowlists, lag and
      version headers, timeouts, caller restrictions.
- [ ] If mTLS: PIP `-tls-cert/-tls-key/-client-ca`; sidecar
      `-pip-ca/-pip-cert/-pip-key/-pip-server-name`.
- [ ] Metrics scrape for `:9090` on sidecar and PIP, restricted to monitoring.

## 4. Deploy order

1. Namespaces and secrets.
2. `kubectl apply` the rendered cluster controls
   (`helm template … | kubectl apply`), or run `Jenkinsfile.cluster-controls`.
3. PIP, then sidecar rollout (service deployment).
4. Render + conformance + sign the first bundle; deploy it.
5. Canary one service; watch deny rate, PIP errors/lag, latency.

## 5. Verification **[env]**

- [ ] `kubectl exec` from an unlabeled pod to the PIP is blocked.
- [ ] A request without a client certificate is rejected when mTLS is on.
- [ ] A caller outside `allowedCallers` gets 403.
- [ ] `GET /metrics` shows decisions, PIP cache, lag and bundle info.
- [ ] Bundle reload: change a test bundle, observe `bundle reloaded`; corrupt
      it, observe last-known-good.
- [ ] `make smoke` against a staging ingress.
- [ ] Load test at 1,200 RPS/pod: p99 within budget, zero errors.

## 6. Rollback

- Bundle: re-apply the previous archived `cluster-controls.yaml` / previous
  bundle digest; no app rebuild.
- Sidecar: previous image digest; rolling restart.
- PIP: previous image; keep 2+ replicas during rollout.
- Policy: previous signed bundle; verify digest in decision logs.

## 7. Open risks

- **PIP read APIs** are new surface: review their auth, rate limits and PII
  minimisation.
- **Caller identity** depends on certificate SANs being issued correctly.
- **Bundle signatures** are only enforced when `-bundle-pubkey` is set; make it
  mandatory in production manifests.
- **Dependency scanning** (`govulncheck`) must run in CI; it could not run in
  the build environment.
- **Cedar/OPA** engines are optional; benchmark them before high-RPS use.
