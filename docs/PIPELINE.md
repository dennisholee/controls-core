# Pipeline contract (Jenkins)

The pipeline is **centrally managed**: Jenkins owns orchestration, credentials,
signing, artifact storage, promotion and deployment. Core engineering owns the
two pipeline-invocable tools and the formats they produce. The delivery team
owns the inputs (`policies/`, `registry/`, `dq/`, `mask-policies/`, overlays).

Jenkins must not re-implement merging or validation. It calls
`bundle` and `conformance` from a pinned tools image.

## Tools image

Built from [`../deploy/Dockerfile.tools`](../deploy/Dockerfile.tools) and
published to the internal registry. Pin by digest in Jenkins:

```groovy
agent {
  docker {
    image 'registry.example.com/controls-tools@sha256:<digest>'
    args '-u root'
  }
}
```

Contains: `bundle`, `conformance`, `openapi-diff`.

## Inputs

| Input | Location | Owner |
|---|---|---|
| Policies, mask policies, registry, DQ rules | `policies/`, `mask-policies/`, `registry/`, `dq/` | Delivery |
| Environment overlay | `env/<env>/overrides.json` | Delivery |
| Service overlay (optional) | `services/<service>/overrides.json` | Delivery |
| Service OpenAPI (optional gate input) | `services/<service>/openapi.yaml` (JSON also accepted) | Service team |
| Fixtures (conformance) | `tests/fixtures/*.json` | Delivery |

Endpoint registry entries carry `service`, which is what makes per-service
bundles possible.

## Overlay schema

Only these keys are applied; anything else is ignored by the renderer and
should not be used.

```json
{
  "environment": "prod",
  "bundle": {
    "defaultUnregistered": "deny",
    "disableRules": ["rule.id"],
    "disableDQRules": ["dq.rule.id"],
    "disableEndpoints": ["endpoint-id"],
    "maskPolicies": {
      "journey.account.read": { "default": "full", "fields": {} }
    }
  }
}
```

Unknown disable ids are errors — typo protection. Mask policy overrides must
name an existing policy and are validated again by the engine's bundle loader
(including locked fields).

## Stages

```groovy
stage('Render') {
  sh "bundle -src . -env ${params.ENV} -service ${params.SERVICE} -out dist/${params.SERVICE}"
}
stage('Conformance') {
  sh "conformance -bundle dist/${params.SERVICE} -fixtures tests/fixtures -service ${params.SERVICE} -v"
}
stage('OpenAPI gate') {
  when { expression { fileExists("services/${params.SERVICE}/openapi.yaml") || fileExists("services/${params.SERVICE}/openapi.json") } }
  script {
    def spec = fileExists("services/${params.SERVICE}/openapi.yaml") ?
      "services/${params.SERVICE}/openapi.yaml" : "services/${params.SERVICE}/openapi.json"
    sh "openapi-diff -openapi ${spec} -registry dist/${params.SERVICE}/registry.json"
  }
}
stage('Archive') {
  archiveArtifacts artifacts: "dist/${params.SERVICE}/**", fingerprint: true
}
```

Reference: [`../ci/Jenkinsfile`](../ci/Jenkinsfile).

## Outputs

```
dist/<service>/
  rules.json
  mask-policies.json
  registry.json
  dq.json
  manifest.json        # bundleDigest, bundleVersion, schemaVersion, counts, sources
  manifest.sig         # Ed25519 signature over the recomputed bundle digest
```

`manifest.json` is the handoff to sign, store and deploy. The sidecar logs
`bundleVersion`/digest on every decision, so an incident can be tied to an exact
bundle.

## Exit codes

| Tool | Code | Meaning |
|---|---|---|
| `bundle` | 0 | rendered and validated |
| `bundle` | 1 | invalid source, unknown overlay key/id, validation failure |
| `bundle` | 2 | usage error |
| `conformance` | 0 | all fixtures passed |
| `conformance` | 1 | a fixture failed or the bundle could not be loaded |
| `openapi-diff` | 0 / 1 | in sync / drift |

Jenkins should fail the build on any non-zero exit and keep the renderer and
conformance output as build artifacts.

## Signing, publishing, promotion

Owned by Jenkins, not by these tools:

1. Render + conformance pass on the PR (fast feedback).
2. Merge to `main` triggers a build for each service/environment.
3. Sign the bundle with the pipeline's Ed25519 key:
   `bundle-sign -bundle dist/<service> -key "$BUNDLE_SIGNING_KEY"`.
4. Verify the signature and record the digest.
5. Publish the bundle as an OCI artifact (or the platform's artifact store).
6. Deploy by pinning the bundle digest in the service's deployment and the
   public key into the sidecar (`-bundle-pubkey`).
7. Rollback redeploys the previous digest — no application rebuild.

### Keys

```bash
bundle-keygen -out ./keys        # once, offline
```

Store the private key in Jenkins credentials (file credential). Mount only the
public key into the sidecar. Losing or rotating the key requires re-signing the
bundle; the sidecar refuses to start when verification fails.

## Cluster controls job

The bundle pipeline does not deploy the PIP or the network/mTLS objects. A
second, Jenkins-owned job renders the chart and applies it with `kubectl`:

```bash
helm template controls deploy/helm/controls-plane --namespace controls \
  ${HELM_ARGS} > cluster-controls.yaml
kubectl diff -f cluster-controls.yaml || true
kubectl apply -n controls -f cluster-controls.yaml
```

- Helm is a renderer only; there is **no Helm release state**.
- Jenkins archives `cluster-controls.yaml`; rollback re-applies the previous
  build's artifact.
- The job needs a kubeconfig credential and permission to create
  NetworkPolicies (and Istio objects when enabled).
- The chart does not create the audit-salt or bundle-public-key secrets; the
  service deployment mounts those.

Reference: [`../ci/Jenkinsfile.cluster-controls`](../ci/Jenkinsfile.cluster-controls).

## Versioning

- Pin the tools image by digest; do not use `latest`.
- The renderer records `schemaVersion` (contract major/minor) in the manifest.
- The sidecar refuses incompatible contract majors (see
  [`../docs/COMPATIBILITY.md`](../docs/COMPATIBILITY.md)).
- Upgrading the tools image and the sidecar is a normal two-step change:
  publish contracts → deploy sidecar → render new bundles.

## What Jenkins must not do

- Merge global/environment/service configuration itself.
- Skip conformance because "the rules are simple".
- Relax locked mask fields or run `defaultUnregistered: allow` in production.
- Build rules into the application image; the bundle is an independent artifact.
