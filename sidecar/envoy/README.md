# Envoy configuration

Reference configuration for wiring the controls `ext_proc` service into Envoy.
The exact deployment depends on whether the platform runs Istio or standalone
sidecars; both modes use the same filter settings.

## Filter settings

```yaml
http_filters:
  - name: envoy.filters.http.ext_proc
    typed_config:
      "@type": type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
      grpc_service:
        envoy_grpc:
          cluster_name: controls_ext_proc
        timeout: 1s
      processing_mode:
        request_header_mode: SEND
        response_header_mode: SEND
        request_body_mode: BUFFERED
        response_body_mode: BUFFERED
        request_trailer_mode: SKIP
        response_trailer_mode: SKIP
      message_timeout: 1s
      failure_mode_allow: false
  - name: envoy.filters.http.router
    typed_config:
      "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
```

- `request_body_mode` / `response_body_mode: BUFFERED` — 4 KB p99 makes buffering
  the simplest correct choice.
- `failure_mode_allow: false` — if the ext_proc service is unavailable, Envoy
  fails closed rather than passing traffic uncontrolled.
- `message_timeout: 1s` — the sidecar's own PIP timeout is 100 ms; the Envoy
  timeout is the outer bound.
- Strip `Accept-Encoding` for JSON routes (a small Lua/header mutation or the
  app's configuration) to avoid decompress/recompress.

## Standalone sidecar

A complete static listener is in [`static-sidecar.yaml`](static-sidecar.yaml):

- Envoy listens on `0.0.0.0:8080`, forwards to the app on `127.0.0.1:18080`.
- The app must bind loopback and change port; Envoy takes the public port.
- `controls_ext_proc` is the local ext_proc service (`127.0.0.1:50051`).

## Istio

Use an `EnvoyFilter` patch on `HTTP_FILTERS` to insert the ext_proc filter
before the router, with `grpc_service.envoy_grpc.cluster_name` pointing at a
cluster for the sidecar's loopback ext_proc port. Keep the same
`processing_mode` and `failure_mode_allow` values.

## Bypass rules

- SSE / streaming responses cannot be buffered. Exclude those routes from the
  ext_proc filter (separate listener or `typed_per_filter_config` with
  `DISABLE`) and record the exclusion in the registry review.
- Non-JSON bodies pass through the engine's mask phase unchanged; sensitive
  non-JSON endpoints must be blocked at the gateway.

## Body size

The adapter rejects request bodies over `-max-body-bytes` (default 1 MiB) with
413, and refuses to return a response larger than the limit when masking is
required. Calibrate against the 4 KB p99, not the maximum ever observed.
