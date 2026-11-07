# controls-plane Helm chart

Reference deployment for the PIP plus the network/mTLS controls that make the
sidecar safe to run. The sidecar itself is injected into service pods by the
service's chart; this chart provides the policies those pods are subject to.

## Render and apply

Helm is used as a **renderer**: Jenkins applies the output with `kubectl`; there
is no Helm release state. Archive the rendered file and re-apply the previous
build's manifest to roll back.

```bash
kubectl create namespace controls --dry-run=client -o yaml | kubectl apply -f -
helm template controls deploy/helm/controls-plane --namespace controls \
  --set pip.image=registry.example.com/controls-pip \
  --set pip.tag=0.10.1 > cluster-controls.yaml
kubectl apply -n controls -f cluster-controls.yaml
```

`make cluster-controls` renders the same file. The reference job is
[`ci/Jenkinsfile.cluster-controls`](../../../ci/Jenkinsfile.cluster-controls).

With Istio strict mTLS and PIP authorization, add the same flags to the render:

```bash
helm template controls deploy/helm/controls-plane --namespace controls \
  --set istio.enabled=true \
  --set istio.authorizationPolicy.allowedPrincipals[0]=cluster.local/ns/payments/sa/accounts \
  > cluster-controls.yaml
```

## What it enforces

| Finding | Control |
|---|---|
| C1 — PIP reachable without identity | NetworkPolicy admits only pods labeled `controls-plane/sidecar=true`; optional Istio `PeerAuthentication: STRICT` and an `AuthorizationPolicy` limited to listed service-account principals |
| C2 — sidecar identity spoofing | NetworkPolicy admits the sidecar's app port only from the gateway namespace/pod selector; the sidecar must still strip/re-inject identity headers at the gateway |
| H1 — unsigned bundles | The sidecar receives `-bundle-pubkey`; the chart mounts only the public key |
| L2 — open metrics | Metrics port is admitted only from the monitoring namespace selector |

## Required configuration

- `sidecar.podLabels` must match the labels service charts put on pods.
- `networkPolicy.sidecar.gatewayNamespaceSelector` / `gatewayPodSelector` must
  match the real ingress gateway. The defaults are placeholders.
- If sidecars live outside the release namespace, set
  `networkPolicy.pip.sidecarNamespaceSelector`.
- Set `sidecar.bundlePublicKeySecret` so the sidecar can verify bundles.
- `pip.egress.databaseCIDRs` must list the read replicas.

## Caveats

- NetworkPolicy is enforced only by a CNI that supports it (Calico, Cilium,
  Antrea, …). Verify with `kubectl describe networkpolicy`.
- This chart does not authenticate users; the gateway owns authentication and
  must strip client-supplied `x-subject`/`x-claim-*` before injecting verified
  values.
- The reference PIP image serves an in-memory fixture. Production uses
  read-replica adapters (see `controls-core/pip/README.md`).
- The sidecar container/Envoy configuration is provided by the service chart;
  `values.yaml` documents the ports and secrets it must use.
