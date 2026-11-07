# Testing the solution through the HTTP API

The controls engine can be exercised without Envoy, a gateway, a database or a
Kubernetes cluster. `sidecar/cmd/sidecar` runs the **same engine** as the
production `ext_proc` adapter and exposes it over HTTP.

This is a test interface, not a production entrypoint: identity comes from
`x-subject`/`x-claim-*` headers instead of the gateway trust chain. It binds to
loopback by default and must never be exposed to untrusted clients.

- API contract: [`../contracts/test-api.yaml`](../contracts/test-api.yaml)
- Browser console: `http://127.0.0.1:8081/docs`
- One command: `make api`

## Verify without starting a server

```bash
make testdrive
```

This builds the test API, starts it on loopback, evaluates one masked account
read, asserts the redaction, and exits. It is the fastest way to confirm the
local bootstrap before using the interactive console.

## Quick start

```bash
make api
# controls test API listening  addr=127.0.0.1:8081  pip=fixtures
```

In another terminal:

```bash
curl -s localhost:8081/v1/process -H 'content-type: application/json' -d '{
  "request": {"method":"GET","path":"/accounts/A123",
              "headers":{"x-subject":"u-1","x-claim-region":"US"}},
  "response": {"status":200,"body":{
    "accountNumber":"1234567890",
    "holder":{"email":"jane.doe@example.com"},
    "ssn":"111-22-3333"}}
}' | jq
```

```json
{
  "decision": {
    "allowed": true,
    "status": 200,
    "reason": "allow:account.read.owner-or-region",
    "endpointId": "get-account",
    "resourceType": "account",
    "resourceId": "A123"
  },
  "response": {
    "status": 200,
    "body": {
      "accountNumber": "******7890",
      "holder": {"email": "j***@example.com"},
      "ssn": "[REDACTED]"
    }
  }
}
```

Open `http://127.0.0.1:8081/docs` for a form-based console, or import
`http://127.0.0.1:8081/openapi.yaml` into Postman/Insomnia.

## What runs

```
make api
  ├─ loads a bundle directory (registry, rules, mask policies, DQ)
  ├─ serves the PIP from pip/fixtures/attributes.json in-process
  └─ exposes the engine on 127.0.0.1:8081
```

`make api` uses `integration/testdata/config` by default. Override the bundle
without editing the Makefile:

```bash
BUNDLE_DIR=dist/accounts make api
```

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness; fails closed if the policy engine did not initialise |
| `GET` | `/v1/info` | Bundle version/digest, engine, PIP mode, counts |
| `GET` | `/v1/endpoints` | Registered routes with their masking configuration |
| `POST` | `/v1/process` | Evaluate a request and mask the supplied upstream response |
| `POST` | `/v1/test` | Run a conformance fixture; returns pass/fail checks |
| `GET` | `/openapi.yaml` | OpenAPI contract |
| `GET` | `/docs` | Browser console |

## Requests

The envelope has three optional parts: the request under test, the upstream
response to mask, and an explicit subject. Without an explicit `subject`,
identity comes from `x-subject` and `x-claim-<name>` headers.

```json
{
  "request": {
    "method": "GET",
    "path": "/accounts/A123",
    "headers": {"x-subject": "u-1", "x-claim-region": "US"},
    "body": null
  },
  "response": {"status": 200, "body": {"ssn": "111-22-3333"}},
  "subject": {"sub": "u-1", "claims": {"region": "US"}}
}
```

- `response` is optional. Omit it to test a decision only.
- The response body is masked **only when the request is allowed**. A denied
  request never receives an upstream body, matching production.
- `request.path` may include a query string; it is stripped before registry
  matching.
- Unknown top-level fields are ignored, so a full conformance fixture can be
  posted to `/v1/process` as well.

### Allowed

```bash
curl -s localhost:8081/v1/process -H 'content-type: application/json' -d '{
  "request":{"method":"GET","path":"/accounts/A123",
             "headers":{"x-subject":"u-1","x-claim-region":"US"}},
  "response":{"status":200,"body":{"ssn":"111-22-3333"}}
}' | jq .decision
```

### Denied

```bash
curl -s localhost:8081/v1/process -H 'content-type: application/json' -d '{
  "request":{"method":"GET","path":"/accounts/A123",
             "headers":{"x-subject":"u-2","x-claim-region":"EU"}}
}' | jq .decision
# {"allowed":false,"status":403,"reason":"default:deny(no-allow-match)",
#  "endpointId":"get-account","resourceType":"account","resourceId":"A123"}
```

### DQ failure

A blocking DQ rule denies with `dq_blocking:<rule>`:

```bash
curl -s localhost:8081/v1/process -H 'content-type: application/json' -d '{
  "request":{"method":"POST","path":"/payments",
             "headers":{"x-subject":"u-1"},
             "body":{"_id":"P-1","_type":"payment","ownerId":"u-1"}},
  "response":{"status":201,"body":{"id":"P-1"}}
}' | jq .decision
```

The integration bundle requires `amount` to be present and greater than zero
for `payment` writes, so the request above is denied with
`dq_blocking:payment.amount.required`. Add `"amount":10` to the request body to
see an allow.

## Conformance fixtures

`POST /v1/test` accepts the same fixture format used by `tools/conformance`
and stored in `controls-rules/tests/fixtures`:

```bash
curl -s localhost:8081/v1/test \
  -H 'content-type: application/json' \
  --data-binary @../controls-rules/tests/fixtures/get-account-owner.json | jq
```

```json
{
  "name": "account-owner-masked",
  "pass": true,
  "decision": {"allowed": true, "status": 200, "...": "..."},
  "response": {"status": 200, "body": {"accountNumber": "******7890", "...": "..."}},
  "checks": [
    {"check": "allowed", "ok": true, "got": true, "want": true},
    {"check": "status", "ok": true, "got": 200, "want": 200},
    {"check": "masked.accountNumber", "ok": true, "got": "******7890", "want": "******7890"}
  ]
}
```

- HTTP status is `200` whether the fixture passes or fails; inspect `pass` and
  `checks`.
- The fixture's `pip` block overrides the running PIP for those resources.
  Resources it does not declare fall back to the running PIP.
- `expect.masked` uses dot notation (`holder.email`).
- `expect.reason` is optional; omit it unless the reason is part of the
  contract you are testing.

This makes it possible to test a delivery change end to end with one HTTP call,
without rebuilding the sidecar or rendering a bundle.

## Testing the delivery-owned rules

`make api` serves the reference bundle under `integration/testdata/config`. To
test the real rules from `controls-rules`:

```bash
make api-rules ENV=dev SERVICE=accounts
```

That target renders `../controls-rules` with `tools/bundle` into
`dist/accounts`, then serves that bundle with the same in-process PIP fixtures.
The equivalent manual steps:

```bash
make build
./bin/bundle -src ../controls-rules -env dev -service accounts -out dist/accounts
BUNDLE_DIR=dist/accounts make api
```

If a rule references resource attributes the fixtures do not have, add them to
`pip/fixtures/attributes.json` or run the external PIP via `make pip` +
`make sidecar`.

## External PIP mode

`make api` embeds the fixture PIP. To exercise the real PIP contract instead:

```bash
make pip      # 127.0.0.1:8082
make sidecar  # 127.0.0.1:8081, talks to the PIP over HTTP
```

The PIP API is documented in [`../contracts/pip-api.yaml`](../contracts/pip-api.yaml)
and can be called directly:

```bash
curl -s 127.0.0.1:8082/v1/attributes:batch -H 'content-type: application/json' -d '{
  "requests":[{"type":"account","id":"A123","fields":["ownerId","region"]}]
}' | jq
```

## Security and limits

- **Loopback only by default.** Binding anywhere else logs a warning. The
  harness trusts `x-subject`; use it only on a developer machine or an isolated
  CI network.
- **No mTLS and no gateway checks.** Those are production-only controls. The
  test API starts after the bundle, so it exercises policy and masking, not
  transport authentication.
- **Request bodies are capped** at 1 MiB (`-max-body-bytes`).
- **No bundle reload.** Restart the process after editing a bundle. The
  production sidecar reloads and verifies signed bundles; see
  [`../sidecar/README.md`](../sidecar/README.md).
- **JSON only for masking.** A masked endpoint returns `mask_failed` for a
  non-JSON upstream body unless the endpoint sets `allowNonJSON`.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `invalid controls bundle` at startup | Bundle directory is missing a file or failed validation; run `./bin/conformance -bundle <dir>` or read the error. |
| `403 unregistered_endpoint` | No registry entry matches method + path. Check `/v1/endpoints`. |
| `404 resource_not_found` | The PIP fixtures have no attributes for that type/id. |
| `503 policy_init_error` | Cedar/OPA engine failed to initialise; `/readyz` shows the error. |
| `mask_failed` | The masked endpoint returned non-JSON, or classification lookup failed. |
