# SPEC — Data-quality rules

Status: normative. Schema: [`dq.schema.json`](dq.schema.json).
Version: 1.0.

DQ is a request-side control. It evaluates the JSON request body after the
route is bound and entitlement has allowed the request, but before the upstream
service is called. A violation is always recorded in the decision log and
`controls_dq_violations_total`; only rules with `blocking: true` reject the
request.

## 1. Scope

A rule applies when all of its scopes match:

- `resourceType` — the registry entry's resource type, or `*`;
- `action` — `read`, `write`, `delete` or `*` (default); use `write` for
  required/value rules so an empty `GET` body cannot deny a read;
- `endpoint` — optional registry entry id; use it when two journeys share a
  resource type but have different request bodies.

`GET` requests normally have an empty body. Required rules **must** be scoped
to `write` (or a specific write endpoint); an unscoped required rule denies
reads.

## 2. Fields and paths

`field` is a dot path into the request body:

- `legalName.firstName`;
- `contactPoints.email.value`;
- `addresses[*].country` — array wildcard.

A wildcard rule evaluates every element. `required` treats a missing array,
an empty array, or any empty element as a violation. A value rule reports one
violation if any element fails.

## 3. Operators

| `op` | Meaning | Required property |
|---|---|---|
| `required` (flag) | field present and non-empty | — |
| `pattern` | RE2 match against the string form | `pattern` |
| `maxLength` | string length <= `maxLength` | `maxLength` |
| `type` | value is `string` or `number` | `value` |
| `eq`, `neq`, `gt`, `gte`, `lt`, `lte` | comparison | `value` |

Missing fields are skipped by value rules; `required` is the only presence
check. Unknown operators, invalid regexes and malformed paths fail at bundle
load, not at request time.

## 4. Blocking and reporting

- `blocking: true` → `403` with reason `dq_blocking:<rule.id>`.
- `blocking: false` (default) → request continues; the violation is recorded.
- Multiple blocking violations: the first in bundle order determines the
  reason. All evaluated violations are included in the decision record.

## 5. Example

```json
{
  "version": "1.0",
  "rules": [
    {
      "id": "party.legalName.firstName.required",
      "resourceType": "party",
      "action": "write",
      "endpoint": "update-party",
      "field": "legalName.firstName",
      "required": true,
      "blocking": true
    },
    {
      "id": "party.addresses.country.format",
      "resourceType": "party",
      "action": "write",
      "endpoint": "update-party",
      "field": "addresses[*].country",
      "op": "pattern",
      "pattern": "^[A-Z]{2}$",
      "blocking": true
    }
  ]
}
```

## 6. Validation gates

1. JSON Schema validation (`dq.schema.json`).
2. Semantic validation: known resource type/action, endpoint exists and
   matches the rule scope, field exists in that endpoint's request schema.
3. Rule compilation: operator, regex and path validity.

`controls-check` performs all three.
