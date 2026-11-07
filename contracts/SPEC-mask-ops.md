# SPEC — Mask Operations

Status: normative. Schema: [`mask-policy.schema.json`](mask-policy.schema.json).
Version: 1.0.

Masking is the enforcement point for **field-level data entitlement**. A mask
policy is selected by the endpoint registry and applied to the JSON response
(including error responses when `maskErrors` is true).

## 1. Modes

| Mode | Result on string | Result on non-string |
|---|---|---|
| `none` | unchanged | unchanged |
| `full` | `"[REDACTED]"` | `null` |
| `partial` | keeps configured characters, masks the rest with `*` | stringifies, then masks |

`partial` is only valid when at least one of `keepPrefix`, `keepSuffix`,
`preserveDomain` or `keep` is set. A bare `partial` is a validation error —
"partial" must never be an ambiguous default.

### partial parameters

- `keepPrefix: n` — keep the first `n` characters.
- `keepSuffix: n` — keep the last `n` characters.
- `keepPrefix + keepSuffix >= len` — mask the whole value (`*` repeated).
- `preserveDomain: true` — email local part is masked, `@domain` preserved.
  `keepPrefix` applies to the local part; default is `***@domain`.
- `keep: "year"` — keep the first four characters, mask the rest (dates).

## 2. Locked fields

The following field names are **locked to `full`** and cannot be relaxed by any
journey:

`ssn`, `pan`, `cvv`, `cardNumber`

Bundle validation rejects any policy that sets a locked field to `none` or
`partial`. This is enforced in code (`config.LockedFields`) and must also be
enforced by the delivery pipeline before merge.

## 3. Defaults

`default` applies to leaf fields not named in `fields`:

- `none` (default) — unlisted fields pass through.
- `full` — all unlisted leaf fields are masked. Use for strict journeys; it
  will mask non-sensitive fields too, which is intended.

### Classification fallback

`fallback: "classification"` masks every field the PIP tags as PII
(`pii.*`) that the policy did not explicitly list. Explicit field rules always
win, so a partial rule on `accountNumber` is not overridden by its `pii.partial`
tag. Unclassified fields are untouched.

If fallback is enabled and classification metadata cannot be fetched and is not
cached, masking fails closed: the response is denied rather than returned
unmasked. This is the fail-safe path for PIP outages on endpoints that do not
need instance attributes.

## 4. Paths

- Dot notation: `holder.email`, `accountNumber`.
- Array wildcard: `items[*].price` masks `price` on every element.

## 5. Write paths

`rejectFields` lists request-body fields that must never be accepted in
cleartext (for example `pan`, `cvv`). Presence causes a `403 rejected_field:<name>`
before the request reaches the application. Writes are rejected, not silently
masked, so clients cannot believe an update succeeded.

## 6. Non-JSON bodies

When a mask policy applies, a non-JSON response is **rejected** (the request
fails closed) unless the endpoint sets `allowNonJSON: true`. This prevents a
content-type or serialization change from silently bypassing masking. Empty
responses are always allowed. Any endpoint opting out must be reviewed as
non-sensitive.

## 7. Failure behaviour

If masking cannot be applied because the mask policy is missing or the bundle is
invalid, the sidecar fails closed: the request is rejected rather than the
response returned unmasked. If the PIP is unavailable, entitlement denies and
the response is never produced.

When a policy enables `fallback: classification`, classification metadata is
read from the sidecar cache first (hours TTL) and fetched from the PIP only on a
miss. A fetch failure fails closed. See §3.1.

## 8. Test obligations

Every mask policy needs fixture tests asserting:

1. each masked field's exact output;
2. unlisted fields are unchanged (or masked, when `default: full`);
3. nested paths and array wildcards;
4. error-response masking when `maskErrors` is true;
5. locked fields rejected by validation when set to `partial`/`none`.
