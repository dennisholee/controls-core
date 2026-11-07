# BDD and acceptance criteria

Status: adopted for the reference implementation (`v0.37.0`). Owner: core
engineering for the harness, delivery for the journeys.

## 1. Why BDD here

The risky behaviour in this system is a business decision, not a function:
*which route maps to which resource, who may read it, which fields are
returned*. A unit test can prove `mask.Apply` is correct and still miss that the
registry bound `/accounts/{id}` to the wrong mask policy.

The repository already has the ingredients:

| Ingredient | Where it lives today |
|---|---|
| Business expectations | `controls-rules/tests/fixtures/*.json` (`expect.allowed`, `expect.masked`) |
| Acceptance logic | `controls-core/integration/e2e_test.go` |
| End-to-end verification | `controls-core/hack/envoy-smoke.sh` (`make smoke`) |

What was missing is a readable, reviewable statement of *what* must be true,
shared by delivery and core, and executed at more than one level. The feature
files are that statement.

## 2. The model

```
features/*.feature              business language, one file per journey
        │
        ▼
integration/acceptance_test.go  step definitions, transport-agnostic world
        │
        ├── engine backend      in-process: engine + fixture PIP
        └── HTTP backend        live Envoy -> ext_proc -> engine -> upstream
```

One acceptance criterion, two executions:

| Backend | Selected by | Proves |
|---|---|---|
| Engine | default (`make acceptance`, `make test`) | decision, reason, masking math |
| HTTP | `CONTROLS_ACCEPTANCE_URL` (set by `make smoke`) | the criterion survives Envoy buffering, headers and the real upstream |

Scenarios tagged `@engine` assert internals that are not on the wire (decision
reasons) and are skipped by the HTTP backend. A feature tagged `@e2e` means its
scenarios should be observable through the real entrypoint.

The Gherkin reader is deliberately a small subset: `Feature`, `Background`,
`Scenario`, `@tags`, `Given/When/Then/And/But/*`, data tables and doc strings.
Scenario outlines are rejected with an error rather than silently mis-parsed.
If the suite outgrows this, replace `integration/gherkin_test.go` with
[cucumber/godog](https://github.com/cucumber/godog); the step definitions and
feature files survive that swap.

## 3. Definition of done (acceptance criteria)

A change to the controls plane is accepted when all of the following hold.

### Per endpoint

1. An **allow + mask** scenario exists and asserts every masked field by exact
   value (not "contains `*`").
2. A **deny** scenario exists and asserts the status.
3. **Unregistered route** behaviour is covered once per service.
4. Every `selfDescribing` endpoint has a **type mismatch** and an **id
   mismatch** scenario.
5. Every `rejectFields` entry has a scenario naming the rejected field.
6. Every **blocking DQ rule** has a deny scenario naming the rule.
7. A **PIP outage** fails closed (503) with no unmasked body.

### Per journey

8. A sanitised-subject deny (for example `status=sanctioned`) is covered when a
   deny rule applies across resource types.
9. Error-body masking is covered for endpoints with `maskErrors: true`.
10. Each scenario is **falsifiable**: reverting the policy, registry entry or
    engine change that the scenario protects must make it fail. A scenario that
    cannot fail is documentation, not a test.

### Gates

11. `make acceptance` passes against the in-process engine.
12. `make smoke` passes and runs the same `@e2e` scenarios through Envoy.
13. `make test`, `make vet`, `make fmt-check`, `make openapi-gate` remain green.
14. A new registry entry without a feature scenario fails review; the reviewer
    asks for the scenario, not for a fixture alone.

Criteria 1–10 are the review checklist. 11–13 are automated gates.

## 4. Running

```bash
cd controls-core
make acceptance                 # engine backend, verbose scenario names
make test                       # includes TestAcceptance
make smoke                      # engine + live Envoy BDD, Docker required

# Against any already-running HTTP entrypoint (Envoy listener or a gateway
# that forwards the same headers):
CONTROLS_ACCEPTANCE_URL=http://127.0.0.1:18000 \
  go test ./integration -run TestAcceptance -count=1 -v
```

## 5. Adding or changing a criterion

1. Delivery writes the scenario in business language, in the journey file,
   before the rule or mask change is merged. Use the shared vocabulary
   (`subject`, `endpoint`, `masked`, `denied`), not framework terms.
2. Core adds a step definition only if an existing one does not express it. Step
   definitions are a small, reviewed vocabulary; a new step is a contract
   change.
3. Put the data in a fixture when the same values are reused by the delivery
   pipeline. The feature asserts behaviour; the fixture supplies data.
4. Run `make acceptance`. If the scenario fails for an expected reason, the
   implementation is not done.
5. Run `make smoke` before release.

Rules of the road:

- One feature per journey (`account-read`, `payment-write`), not per class or
  package.
- No environment conditionals inside features. If a scenario needs a different
  backend, tag it, do not branch the text.
- Do not weaken an assertion to make it pass. Remove or rewrite the scenario
  with a linked reason in the PR.
- Paths and claims are part of the external contract and may appear in
  scenarios; internal types and function names must not.

## 6. Should an agent own this?

**Yes, one accountable agent: a conformance steward.** It should own the
acceptance layer the way an SRE owns an SLO: continuously, mechanically, and
with explicit decision rights. It should *not* own the business intent.

Why an agent rather than a human rotation:

- The checks are repetitive and high-frequency (every PR, nightly, release).
- Drift is cross-repo: `controls-rules` changes a rule, `controls-core` owns the
  feature and the engine. Someone has to reconcile the two on every change.
- Failure classification is pattern work: spec drift vs config drift vs engine
  regression.

Why not an agent that authors both the expectation and the implementation: that
is a tautology. The agent would encode whatever the code does and call it
correct. The *what* stays with delivery (business rules, masking) and core
(engine semantics); the agent enforces that the *what* is expressed and
verified.

Charter: [`docs/agents/conformance-steward.md`](agents/conformance-steward.md).

## 7. Adoption plan

| Phase | Scope | Exit signal |
|---|---|---|
| 1 (done) | Feature files for the two reference journeys, engine + Envoy backends, CI gate | `make acceptance` runs in CI; `make smoke` runs the same `@e2e` scenarios through Envoy |
| 2 | Make `Given the delivery fixture "<name>"` load `controls-rules/tests/fixtures` directly; run the suite against the **rendered** bundle, not `integration/testdata/config` | delivery pipeline fails on a rule without a scenario |
| 3 | Per-service feature packs generated from the registry; mutation check that each scenario fails when its rule is removed | coverage report posted on every rules PR |

Phase 2 is the important one: today the features and the delivery fixtures carry
overlapping data. Unifying them removes the last place where a rule can change
without an acceptance criterion noticing.

## 8. Limits

- The HTTP backend observes status and masked body, not decision reasons. That
  is intentional: reasons are an internal contract and may become structured
  error codes later.
- The suite runs sequentially by design; scenario isolation is per-request, and
  parallel PIP servers make failures harder to read than the milliseconds they
  save.
- Gherkin is not a specification language for the rule grammar. The normative
  rule and mask semantics remain in `contracts/`.

## Status assertions

Engine scenarios assert only decision statuses (`403`, `503`). Upstream status
codes (200/201/500) are asserted by `make smoke` against the live entrypoint;
the in-process engine has no upstream HTTP response to observe, so asserting a
scenario-declared value there would be tautological.
