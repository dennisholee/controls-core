# Runbook

Audience: on-call for the controls plane (core engineering).

## 1. Alerts

| Alert | Condition | Severity | First action |
|---|---|---|---|
| `PIPErrorRate` | 5xx or timeout > 1% for 5 min | page | §2.1 |
| `PIPLatency` | p99 > 50 ms for 5 min | page | §2.2 |
| `PIPCacheHitLow` | sidecar cache hit < 90% for 15 min | ticket | §2.2 |
| `ReplicaLag` | lag > journey threshold for 5 min | page | §2.3 |
| `ControlLatency` | sidecar p99 overhead > 5 ms for 10 min | ticket | §2.2 |
| `DenyRateSpike` | deny rate > 3× 7-day baseline for 10 min | page | §2.5 |
| `BundleLoadFailure` | any sidecar reload failure | page | §2.4 |
| `SidecarNotReady` | pod not ready > 5 min | page | §2.4 |
| `UnmaskedCanary` | canary detects a sensitive field in clear | page + security | §2.6 |
| `CertExpiry` | mTLS cert < 14 days | ticket | §2.7 |
| `PIPContractMismatch` | startup log reports `PIP API contract mismatch`, or `controls_pip_version_mismatch_total > 0` | page | §2.8 |

## 1a. Metrics mapping

Scrape the ext_proc admin listener (`:9090/metrics` by default) alongside the
application. The alerts above map to these series:

| Alert | Metric |
|---|---|
| `PIPErrorRate` | `rate(controls_decisions_total{reason="pip_unavailable"}[5m]) / rate(controls_decisions_total[5m])` |
| `PIPCacheHitLow` | `sum(rate(controls_pip_cache_total{cache="hit"}[15m])) / sum(rate(controls_pip_cache_total[15m]))` |
| `ReplicaLag` | `controls_pip_lag_seconds` (per resource type) |
| `ControlLatency` | `histogram_quantile(0.99, rate(controls_request_duration_seconds_bucket[5m]))` |
| `DenyRateSpike` | `sum(rate(controls_decisions_total{decision="deny"}[10m]))` |
| Stale denials | `rate(controls_stale_denials_total[5m])` |
| DQ blocking | `rate(controls_dq_violations_total{blocking="true"}[5m])` |
| Bundle version | `controls_bundle_info{bundle_version,bundle_digest}` |
| PIP errors | `rate(pip_request_errors_total[5m])` |
| PIP latency | `histogram_quantile(0.99, rate(pip_request_duration_seconds_bucket[5m]))` |
| PIP misses | `rate(pip_attributes_total{found="false"}[5m])` |
| PIP replica lag | `pip_lag_seconds{resource_type}` |
| PIP overload | `rate(pip_rejected_total{reason="concurrency"}[5m])` |
| PIP contract mismatch | `controls_pip_version_mismatch_total > 0` (startup metric) |

PIP latency is currently visible only as end-to-end control latency; a
dedicated PIP histogram is a follow-up if the alert proves too coarse.

## 2. Procedures

### 2.8 PIP contract mismatch

The sidecar compares `GET /version` `apiVersion` with `pip.apiVersion` at
startup. A mismatch exits before serving. Check:

```bash
kubectl logs deploy/<service> -c controls-extproc | grep 'PIP API contract'
curl -s https://<pip>/version
```

Align the PIP deployment and sidecar config, then roll the sidecar.

### 2.1 PIP outage

1. Confirm from two pods: `curl -s $PIP/healthz` and a batch request.
2. Check PIP replicas, then read-replica health and connection pool saturation.
3. Expect **fail-closed**: all PIP-dependent requests return 503. Do not
   disable controls to restore traffic without security approval.
4. Restore PIP capacity (scale replicas, restart unhealthy pods).
5. If the outage is prolonged, the incident commander may approve a gateway-level
   route block — safer than returning unmasked data.
6. Record: duration, affected endpoints, whether any unmasked response escaped.

### 2.2 PIP latency / low cache hit

1. Check sidecar cache hit rate by endpoint: a sudden drop usually means TTL
   churn, a projection change, or cache-key cardinality growth.
2. Check PIP QPS and DB query time. A miss rate above 5% at 5,000 RPS overloads
   the replicas.
3. Scale PIP replicas; verify connection pool > expected miss QPS.
4. Do **not** increase TTLs to fix latency without checking revocation impact.

### 2.3 Replica lag

1. Identify the lagging replica and the affected resource types.
2. For journeys marked `fresh`/`deny-on-stale`, verify the PIP is routing to a
   healthy replica; otherwise the sidecar should deny.
3. Fail the replica out; do not serve stale entitlement data for high-risk
   journeys.

### 2.4 Bad bundle / crashloop

1. Check the pod logs for `invalid controls bundle` — the message names the
   file and field.
2. Identify the last known-good bundle digest (sidecar logs it at startup and
   per decision).
3. Roll back the ConfigMap/volume to that digest and restart the rollout.
4. If only one service is affected, roll back that service's bundle, not the
   sidecar image.
5. Postmortem: add the failing case to the delivery pipeline validation suite.

### 2.5 Deny spike

1. Check whether a bundle was deployed in the last 30 minutes.
2. Compare decisions by endpoint and reason: policy deny vs PIP 404 vs DQ block.
3. If policy changed unexpectedly, roll back the bundle.
4. If PIP data is wrong (e.g. missing owner), fix the data path — do not add an
   allow rule to restore traffic.
5. If subject claims are missing, check the gateway→sidecar header propagation.

### 2.6 Masking regression

1. Treat as a security incident. Block the affected route at the gateway first;
   do not attempt a live config edit under pressure.
2. Capture the bundle digest and a redacted sample.
3. Roll back the bundle/image; verify masked output on the canary.
4. Notify security and data owner; assess exposure window.

### 2.7 Certificate / trust

1. Verify expiry and SANs for sidecar→PIP mTLS.
2. Rotate using the platform process; restart sidecars in a rolling fashion.
3. Confirm `pip_unavailable` errors stop and decision logs resume.

## 3. Break-glass

Only core engineering holds break-glass authority for the controls plane.

| Action | Allowed? | Notes |
|---|---|---|
| Block a route at the gateway | Yes | Preferred fail-safe |
| Roll back a bundle | Yes | Restores a previously reviewed state |
| Disable masking for a route | **No** | Requires security sign-off and a time-boxed exception |
| Set `defaultUnregistered: allow` | **No** | Prohibited in production |
| Relax a locked field | **No** | Contract violation |
| Disable a blocking DQ rule | With delivery + data owner approval | Logged as a change |

## 4. Post-incident

- Record the bundle digest, image version and effective config in the postmortem.
- Add a regression fixture for every control failure.
- If the incident involved an unmasked response, complete the security
  exposure assessment before closing.
