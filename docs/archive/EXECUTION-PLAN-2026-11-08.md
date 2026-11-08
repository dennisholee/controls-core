> **Historical execution record — completed 2026-11-08.**
> Do not execute this plan again. Remaining work is tracked in
> [`docs/DEPRECATION.md`](../DEPRECATION.md) and the pilot checklist.
> `payments-service-demo` was deleted after its OPA policy was archived.

# DeepSeek execution plan — consolidation & deprecation

Status: **executed 2026-11-08**; remaining items are explicitly scheduled in
`controls-core/docs/DEPRECATION.md`.

> Post-execution cleanup: `payments-service-demo` was deleted on 2026-11-08
> after its OPA policy and git bundle were preserved. The M1-01/M1-03 archive
> instructions below are a historical record, not pending work.

Scope: `controls-core`, `controls-rules`, `payments-service-demo`,
`party-mastering-service` in this workspace.

This is the single actionable list. Execute top-to-bottom. **Milestone 1 is
safe cleanup and should land first.** Milestones 2–4 are separate changes with
their own risk and should not be mixed into the same commit.

## Execution record

| Milestone | Result | Commits |
|---|---|---|
| M1 — consolidation | Done: payments archived, accounts-read gated, payments registry regenerated, assessment archived, inventory added, artifacts cleaned | `f88a845` (controls-core), `57998ed` (payments), root `REPO-INVENTORY.md` |
| M2 — EOL/deprecations | Done: Spring Boot 4.1.2, Generator 7.27, Alpine 3.24 + digests, typed Envoy HTTP options, EOL gate | `4a0d1e0` (controls-core), `eeaa95e` (party), `cdbea52` (payments) |
| M3 — de-duplication | Done: shared `test/lib.sh`, `compose.base.yaml`, example drift gate, entrypoint drift gate | `2155035` (controls-core), `fcac923` (party) |
| M4 — governance | Done: supported-version matrix, deprecation policy/removal schedule, CI checks, Go 1.27 | `2155035`, `dd89a76` (controls-core), `a718aaa` (party) |
| Release | `controls-core v0.41.0` committed and tagged locally | `2140810`, tag `v0.41.0` |
| Cleanup | `payments-service-demo` deleted after the OPA policy and git bundle were preserved | `/tmp/payments-service-demo-final.bundle`; `controls-core/docs/archive/payments-authz.rego` |

Deferred by design: legacy code removal and deleting the archived payments
repo. The OPA scenario was migrated to OPA 1.22.2; these remaining items have
owners and targets in `controls-core/docs/DEPRECATION.md`.

## Operating rules

1. Work in the named repository. One task = one commit (or one doc commit per
   repo for cross-repo tasks).
2. Before starting, run the baseline and record failures:
   ```bash
   cd controls-core && GOCACHE=$PWD/.gocache GOPATH=$PWD/.gopath go test ./...
   cd ../party-mastering-service && make docs-check gate
   ```
3. Do not hand-edit generated code under `target/` or committed registries.
   Annotations and generator are the source of truth.
4. Do not commit `.m2-repo/`, `.gocache/`, `.gopath/`, `.tmp/`, `bin/`,
   `target/`, chart packages or secrets.
5. Stop and report if an acceptance check fails. Do not “fix” an unrelated
   failure in the same task.
6. For Maven in this workspace, if offline:
   ```bash
   mvn -o -Dmaven.repo.local=$PWD/.m2-repo ...
   ```
7. After any registry change, run:
   ```bash
   openapi-registry -check ...
   openapi-diff ...
   controls-check ...
   ```

---

## Milestone 1 — consolidate and remove stale artifacts (execute now)

### M1-01 — Archive `payments-service-demo` as legacy

**Why:** it duplicates the canonical party demo, uses OSS-EOL Spring Boot
3.5.16 and OPA 0.70.0, and its committed registry no longer passes
`openapi-registry -check`.

**Files:**
- `payments-service-demo/README.md`
- `payments-service-demo/ARCHIVED.md` (new)
- root `README.md`

**Steps:**
1. Add `ARCHIVED.md` with: status, date, reason, canonical replacement
   (`party-mastering-service`), and “OPA scenario retained for historical
   reference; do not deploy.”
2. Add a one-line banner at the top of `payments-service-demo/README.md`.
3. Move the payments row out of the active table in root `README.md` into an
   “Archived” section. Keep the link.
4. Do **not** delete the repository yet. Deletion needs explicit approval and
   a decision on preserving the OPA example.

**Acceptance:**
- root README has no active payments demo row;
- `payments-service-demo/ARCHIVED.md` exists and links the party demo;
- `git -C payments-service-demo status --short` is clean;
- OPA files and tests are untouched.

---

### M1-02 — Fix the stale `accounts-read` registry and gate it

**Why:** today `openapi-registry -check` fails for this example. The contract
lacks `x-attributes`/`x-mask-errors`, while the committed registry contains
hand-authored values; the Makefile does not run the drift check.

**Files:**
- `controls-core/examples/accounts-read/openapi.yaml`
- `controls-core/examples/accounts-read/controls/registry.json`
- `controls-core/examples/accounts-read/Makefile`

**Steps:**
1. Add to the GET operation:
   ```yaml
   x-attributes: [ownerId, region, status]
   x-mask-errors: true
   ```
2. Regenerate:
   ```bash
   cd controls-core
   GOCACHE=$PWD/.gocache GOPATH=$PWD/.gopath go run ./tools/openapi-registry \
     -openapi examples/accounts-read/openapi.yaml \
     -service accounts \
     -out examples/accounts-read/controls/registry.json
   ```
3. Add `openapi-registry -check` to `examples/accounts-read/Makefile` `gate`
   before `openapi-diff`, using the same `-service accounts`/`-out`.
4. Run the gate.

**Acceptance:**
- `make -C examples/accounts-read gate` exits 0;
- the committed registry passes `-check` and validates against
  `contracts/registry.schema.json`;
- no hand-edited registry fields remain.

---

### M1-03 — Regenerate the payments demo registry before archiving

**Why:** even archived code should not ship a registry that fails the current
generator. The mismatch is only id casing:
`createPayment`/`getAccount` → `create-payment`/`get-account`.

**Files:**
- `payments-service-demo/controls/registry.json`

**Steps:**
1. Regenerate with the current tool:
   ```bash
   cd controls-core
   GOCACHE=$PWD/.gocache GOPATH=$PWD/.gopath go run ./tools/openapi-registry \
     -openapi ../payments-service-demo/openapi.yaml \
     -service payments \
     -out ../payments-service-demo/controls/registry.json
   ```
2. Run:
   ```bash
   go run ./tools/openapi-registry -check \
     -openapi ../payments-service-demo/openapi.yaml \
     -service payments \
     -out ../payments-service-demo/controls/registry.json
   go run ./tools/openapi-diff \
     -openapi ../payments-service-demo/openapi.yaml \
     -registry ../payments-service-demo/controls/registry.json
   ```
3. Run the demo’s Maven registry test:
   ```bash
   cd ../payments-service-demo && mvn -o -Dmaven.repo.local=$PWD/.m2-repo test
   ```

**Acceptance:**
- both gates exit 0;
- only id casing and formatting change;
- the archived demo is buildable, with an `ARCHIVED` banner.

---

### M1-04 — Archive the historical assessment document

**Why:** `docs/ASSESSMENT-2026-11-07.md` is a v0.16.1 decision record with
blockers that are now remediated. It is still linked as current from README and
CHANGELOG.

**Files:**
- `controls-core/docs/ASSESSMENT-2026-11-07.md` → `docs/archive/`
- `controls-core/README.md`
- `controls-core/CHANGELOG.md`

**Steps:**
1. `mkdir -p controls-core/docs/archive`
2. `git mv docs/ASSESSMENT-2026-11-07.md docs/archive/`
3. Add a top banner: “Historical — v0.16.1. Superseded by
   `docs/SECURITY-AUDIT.md` and `docs/PILOT-CHECKLIST.md`.”
4. Update README/CHANGELOG links to the archive path.
5. `grep -R "ASSESSMENT-2026"` to confirm no stale active link.

**Acceptance:**
- archived file clearly marked historical;
- links resolve;
- no claim that old blockers are current.

---

### M1-05 — Add the repository inventory and canonical source map

**Why:** two demos, five examples and multiple deployment artifacts overlap.
Readers need one canonical map.

**Files:**
- `REPO-INVENTORY.md` (new, workspace root)
- root `README.md`

**Content:**
| Path | Status | Purpose |
|---|---|---|
| `controls-core/` | canonical | engine, PIP, contracts, sidecar |
| `controls-rules/` | canonical | delivery-owned policy/bundle sources |
| `party-mastering-service/` | canonical | full Spring Boot/OpenAPI/Party demo |
| `payments-service-demo/` | archived | historical OPA demo |
| `controls-core/examples/*` | minimal examples | focused feature samples |
| `controls-core/deploy/helm/*` | canonical deploy | production charts |
| `controls-core/deploy/examples/*` | render test | chart render verification |

**Acceptance:**
- every top-level directory has one row and an owner/status;
- root README links the inventory;
- no “two active demos” ambiguity.

---

### M1-06 — Remove generated local artifacts (non-destructive)

**Steps:**
1. Remove ignored/generated artifacts only:
   ```bash
   rm -rf controls-core/bin controls-core/.tmp
   rm -rf controls-core/deploy/examples/sidecar-service/charts
   ```
2. Do **not** delete `.gocache`, `.gopath`, `.m2-repo` automatically; they are
   needed for offline builds. Document their size in `REPO-INVENTORY.md`.
3. Confirm no tracked changes:
   ```bash
   git -C controls-core status --short
   git -C party-mastering-service status --short
   ```

**Acceptance:**
- working trees clean;
- no tracked file deleted;
- generated chart package gone;
- cache sizes documented.

---

### M1-07 — Milestone 1 verification

Run and record exit codes:

```bash
cd controls-core
GOCACHE=$PWD/.gocache GOPATH=$PWD/.gopath go test ./...
make -C examples/accounts-read gate
make -C examples/spring-accounts gate
GOCACHE=$PWD/.gocache GOPATH=$PWD/.gopath go run ./tools/openapi-registry -check \
  -openapi ../payments-service-demo/openapi.yaml -service payments \
  -out ../payments-service-demo/controls/registry.json

cd ../party-mastering-service
make docs-check
make gate
```

**Acceptance:** all exit 0. No e2e stack rebuild is required for M1 unless the
accounts-read runtime is also changed.

---

## Milestone 2 — remove unsupported/deprecated platform features

> Do this in a separate branch/PR after M1. It changes dependency majors and
> container bases; do not combine with cleanup.

### M2-01 — Upgrade Spring Boot and OpenAPI Generator

**Evidence:** both demos use Spring Boot `3.5.16`, whose OSS support ended
**2026-06-30**; current lines are 4.1.2 / 4.0.9. OpenAPI Generator is `7.13.0`;
latest is `7.27.0`.

**Files:** `party-mastering-service/pom.xml`, `payments-service-demo/pom.xml`
(if kept), generated code/tests.

**Steps:**
1. Upgrade the party demo first: Spring Boot `4.1.2`, OpenAPI Generator
   `7.27.0`, Java 21 (or 25 if the team standardizes on it).
2. Add `<hideGenerationTimestamp>true</hideGenerationTimestamp>` to the
   generator config for reproducible builds.
3. Handle Jackson 2→3 migration (Spring Boot 4 / Jackson 3 package changes).
4. `mvn verify`, then `make guide-smoke`.
5. Only then apply the same to payments if it is kept.

**Acceptance:** `mvn verify`, `make gate`, all four runtime suites and image
builds pass; no deprecated API warnings; generated source is deterministic.

**Stop condition:** if Jackson 3/OpenAPI Generator blocks the upgrade, pin the
last supported combination, document the exception and split the migration.

---

### M2-02 — Replace EOL Alpine 3.20 base images

**Evidence:** 17 `FROM alpine:3.20` lines. Alpine 3.20 reached end of support on
**2026-04-01**; 3.23/3.24 are supported.

**Files:** all `Dockerfile*` under `controls-core`, examples,
`party-mastering-service`, `payments-service-demo`.

**Steps:**
1. Pick one supported base strategy:
   - `alpine:3.24` + `apk add openjdk21-jre-headless`, or
   - `eclipse-temurin:21-jre-alpine`.
2. Apply consistently, pin by digest, and keep non-root UIDs.
3. Rebuild all images and run the four demo suites plus `make smoke`.

**Acceptance:** no `alpine:3.20`; images build and run; no root user;
dependency/security scan passes.

---

### M2-03 — Migrate Envoy `http2_protocol_options`

**Evidence:** 9 Envoy configs use `Cluster.http2_protocol_options`; Envoy 1.40
logs a deprecation warning.

**Files:** all `envoy/*.yaml`, `sidecar/envoy/static-sidecar.yaml`,
`hack/envoy-smoke.yaml`.

**Steps:**
1. Replace:
   ```yaml
   http2_protocol_options: {}
   ```
   with the typed extension:
   ```yaml
   typed_extension_protocol_options:
     envoy.extensions.upstreams.http.v3.HttpProtocolOptions:
       "@type": type.googleapis.com/envoy.extensions.upstreams.http.v3.HttpProtocolOptions
       explicit_http_config:
         http2_protocol: {}
   ```
2. Validate each config:
   ```bash
   docker run --rm -v "$PWD:/c" envoyproxy/envoy:v1.40.0 \
     --mode validate -c /c/envoy/envoy.yaml
   ```
3. Run `make test`, `make test-signed`, `make test-lag`, `make test-lag-fixture`
   and `make smoke` in `controls-core`.

**Acceptance:** no Envoy deprecation warning; all e2e suites green.

---

### M2-04 — Add an EOL/version policy gate

**Files:** `controls-core/docs/SUPPORTED-VERSIONS.md` (new),
`controls-core/ci/Jenkinsfile` or GitHub workflow, `party-mastering-service/ci/`.

**Steps:**
1. Record supported Spring Boot, Java, Go, Envoy, Alpine, OpenAPI Generator
   and OPA versions with EOL dates.
2. Add Renovate/Dependabot or a scheduled EOL check.
3. Fail CI when a base image or framework is past OSS support unless an
   explicit exception is recorded.

**Acceptance:** policy exists; CI warns/fails on the next EOL; owner assigned.

---

## Milestone 3 — remove structural duplication

### M3-01 — Extract shared test helpers

**Files:** `party-mastering-service/test.sh`, `test-signed.sh`, `test-lag.sh`,
`test-lag-fixture.sh`; new `test/lib.sh`.

Move `fail`, `req`, `assert_code`, `assert_reason`, cleanup and output helpers
into `test/lib.sh`; source it from all four. Keep each script’s scenario
specific.

**Acceptance:** all four suites pass; no duplicated helper definitions.

### M3-02 — Base compose + overrides

**Files:** `compose.yaml`, `compose.signed.yaml`, `compose.lag.yaml`,
`compose.lag-fixture.yaml`; new `compose.base.yaml`.

Use `extends` or profiles for app/PIP/sidecar, overrides for port, identities,
bundle mounts and PIP config.

**Acceptance:** four stacks start cleanly; `make down` cleans all; no duplicate
service definitions.

### M3-03 — De-duplicate example configs

**Files:** 5 example `envoy/envoy.yaml`, 4 `pip/pip.yaml`, 4 app
`Dockerfile`, 3 `go.mod`, 3 `dq.json`, 3 `rules.json`.

Choose one:
- generate them from `examples/shared` templates, or
- add `make examples-config-check` that normalizes variables and fails on
  unintended drift.

**Acceptance:** one canonical source per asset; drift check in CI.

### M3-04 — Stop copying the sidecar entrypoint

**Files:** `party-mastering-service/sidecar/entrypoint.sh`,
`sidecar/Dockerfile`, `Makefile`.

Either consume the released `controls-sidecar` image by digest, or add a
checksum/drift test against `controls-core/deploy/sidecar/entrypoint.sh`.

**Acceptance:** no unverified copy; image still starts signed and unsigned
local modes.

### M3-05 — Duplicate-file CI guard

Add a script that flags new identical files in known families (Envoy configs,
pip configs, app Dockerfiles, test helpers) unless listed as intentional.

**Acceptance:** CI fails on accidental duplication; intentional shared files
are allowlisted.

---

## Milestone 4 — deprecation and governance

### M4-01 — Publish a deprecation policy

Cover:
- legacy sidecar flag mode (`-config`/`-pip`, etc.);
- raw `x-subject`/`x-claim-*`;
- `allowUnsigned`;
- OPA 0.70 / pre-1.0;
- OpenAPI 3.1 (canonical demo migrated; internal tooling contracts remain 3.0.3).
- Go 1.27.

For each: owner, replacement, removal date, test, and a migration note.

### M4-02 — Decide legacy feature removal

- Remove/disable `allowUnsigned` in all production manifests and Helm defaults.
- Require signed identity in production examples.
- Remove legacy flag mode after all charts/examples use `sidecar.yaml`.
- Remove raw-header examples once signed mode is the default.

**Acceptance:** production-path examples cannot enable the escape accidentally.

### M4-03 — Update inventory and coverage

Update `REPO-INVENTORY.md`, `party-mastering-service/docs/GUIDE-COVERAGE.md`
and the READMEs after M1–M3; run `make docs-check` and `make guide-smoke`.

---

## Final definition of done

- [x] M1 tasks complete; `payments-service-demo` archived and its stale registry
      fixed; `accounts-read` gate green.
- [x] Historical assessment archived; repository inventory is canonical.
- [x] No `alpine:3.20`; no deprecated Envoy field; Spring Boot 4.1.x; Generator
      7.27.x.
- [x] Test helpers, compose stacks, example configs and sidecar entrypoint have
      one source of truth or a drift gate.
- [x] Version/EOL policy and deprecation dates exist.
- [x] `controls-core` `go test ./...`, all four party suites, conformance,
      `make docs-check`, `make guide-smoke` green.
- [x] Each repo committed separately with task IDs in the message.

## References

- Alpine release support: https://alpinelinux.org/releases/
- Spring Boot OSS support: https://endoflife.date/spring-boot
- OpenAPI Generator versions:
  https://repo.maven.apache.org/maven2/org/openapitools/openapi-generator-maven-plugin/maven-metadata.xml
- Envoy cluster protocol options:
  https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/cluster/v3/cluster.proto

## Out of scope for this plan

- Cluster deployment, mTLS, gateway signer and load testing — those remain in
  `controls-core/docs/PILOT-CHECKLIST.md`.
- `payments-service-demo` was deleted 2026-11-08; OPA policy archived and the
  full repository history saved as
  `/tmp/payments-service-demo-final.bundle`.
- Rewriting the sidecar engine or changing runtime semantics.
