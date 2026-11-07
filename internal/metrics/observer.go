package metrics

import (
	"strconv"

	"example.com/controls/core/internal/engine"
)

// Observer records engine decisions as metrics.
type Observer struct {
	reg *Registry
}

// NewObserver builds an observer for a registry.
func NewObserver(reg *Registry) *Observer { return &Observer{reg: reg} }

// Bundle publishes the loaded bundle as an info gauge.
func (o *Observer) Bundle(version, digest, schema string) {
	o.reg.Set("controls_bundle_info", "Loaded controls bundle.",
		map[string]string{"bundle_version": version, "bundle_digest": digest, "schema_version": schema}, 1)
}

// Record implements engine.Hooks.OnDecision.
func (o *Observer) Record(d engine.Decision) {
	decision := "deny"
	if d.Allowed {
		decision = "allow"
	}
	o.reg.Inc("controls_decisions_total", "Decisions by endpoint, outcome and reason.",
		map[string]string{"endpoint": d.EndpointID, "decision": decision, "reason": d.Reason}, 1)
	o.reg.Observe("controls_request_duration_seconds", "Control-plane latency by endpoint.",
		map[string]string{"endpoint": d.EndpointID}, d.Latency.Seconds())

	if d.PIPCache != "" {
		o.reg.Inc("controls_pip_cache_total", "PIP attribute resolutions by cache outcome.",
			map[string]string{"cache": d.PIPCache}, 1)
		o.reg.Set("controls_pip_lag_seconds", "Replica lag reported by the PIP.",
			map[string]string{"resource_type": d.ResourceType}, float64(d.PIPLagSeconds))
	}
	if d.Reason == "stale_attributes" {
		o.reg.Inc("controls_stale_denials_total", "Requests denied because replica lag exceeded the bound.",
			map[string]string{"endpoint": d.EndpointID}, 1)
	}
	for _, v := range d.Violations {
		o.reg.Inc("controls_dq_violations_total", "Data-quality violations by rule.",
			map[string]string{"rule": v.RuleID, "blocking": strconv.FormatBool(v.Blocking),
				"resource_type": d.ResourceType}, 1)
	}
}
