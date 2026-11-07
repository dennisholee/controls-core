# Gateway identity envelope

The sidecar must not trust `x-subject` / `x-claim-*` supplied by an arbitrary
client. When `CONTROLS_IDENTITY_HMAC_KEY` is set, the sidecar ignores those
headers and requires a gateway-signed envelope:

```
x-controls-identity: base64url(payload) + "." + base64url(HMAC-SHA256(key, payload))
```

Payload JSON:

```json
{"sub":"u-123","claims":{"region":"US","tier":"gold"},"iat":1762500000}
```

Rules:

- HMAC-SHA256 over the exact payload bytes, raw URL-safe base64 without padding.
- `iat` is Unix seconds; the sidecar rejects envelopes older/newer than 60s.
- `sub` must be non-empty; claims are copied verbatim.
- Missing, malformed, tampered or stale envelopes are rejected with `403
  identity_untrusted` before any policy evaluation.
- The key is read from `CONTROLS_IDENTITY_HMAC_KEY` only (not a CLI flag), so it
  does not appear in the process list.

The gateway is the only component that knows the key; it strips any inbound
`x-controls-identity`, `x-subject` and `x-claim-*` headers and injects the
envelope after authentication. Rotate the key with a rolling restart of the
gateway and sidecars; keep the old key only for the overlap if the gateway can
dual-sign.

Development without a gateway: leave the variable unset and the sidecar falls
back to raw headers. Do not do this outside a trusted development environment.
