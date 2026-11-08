# Performance testing

The sidecar load test runs a real Envoy → ext_proc → engine chain against the
fixture PIP and smoke app. It is intentionally **not** part of every pull
request.

## Run it

```bash
CONTROLS_LOADTEST=1 \
LOADTEST_RPS=300 LOADTEST_DURATION=15s LOADTEST_P99=200ms LOADTEST_CONCURRENCY=60 \
make smoke
```

Or directly with the generator:

```bash
make loadtest LOADTEST_ARGS="-duration 15s -concurrency 60 -rps 300 -max-p99 200ms"
```

The GitHub Actions workflow is `performance`:

- manual dispatch with RPS/duration/p99/concurrency inputs;
- weekly schedule (Monday 04:00 UTC);
- uploads `performance.log` as an artifact;
- writes a parameter summary to the job summary.

## What it measures

End-to-end latency through the real sidecar path: HTTP/1.1 client → Envoy →
ext_proc gRPC → controls engine → PIP attributes → upstream. It reports p50,
p95, p99, status counts and errors.

## What it does not prove

GitHub-hosted runners are shared, noisy and not capacity-controlled. Use this
as a regression signal. For an SLO or capacity claim, run the same workload on
a dedicated runner against the target cluster and record the environment.

## Baseline policy

Start with 300 RPS / 15s / p99 200ms / concurrency 60 on `ubuntu-latest`.
Raise the target only after several green runs; lower p99 only after measuring
the runner's baseline noise. Do not use the 1,200 RPS reference figure as a CI
gate on shared runners.
