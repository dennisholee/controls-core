package metrics

import (
	"bytes"
	"strings"
	"testing"

	"example.com/controls/core/internal/dq"
	"example.com/controls/core/internal/engine"
)

func snap(t *testing.T, r *Registry) string {
	t.Helper()
	var b bytes.Buffer
	r.Write(&b)
	return b.String()
}

func TestCounterAndLabels(t *testing.T) {
	r := New()
	r.Inc("controls_decisions_total", "Decisions.", map[string]string{"decision": "deny", "endpoint": "get-account"}, 2)
	out := snap(t, r)
	if !strings.Contains(out, `controls_decisions_total{decision="deny",endpoint="get-account"} 2`) {
		t.Fatalf("missing counter:\n%s", out)
	}
	if !strings.Contains(out, "# TYPE controls_decisions_total counter") {
		t.Fatalf("missing type:\n%s", out)
	}
}

func TestHistogramCumulative(t *testing.T) {
	r := New()
	r.Observe("latency", "Latency.", map[string]string{"endpoint": "e"}, 0.0007)
	r.Observe("latency", "Latency.", map[string]string{"endpoint": "e"}, 0.2)
	out := snap(t, r)
	if !strings.Contains(out, `latency_bucket{endpoint="e",le="0.0005"} 0`) {
		t.Fatalf("bucket 0.0005:\n%s", out)
	}
	if !strings.Contains(out, `latency_bucket{endpoint="e",le="0.001"} 1`) {
		t.Fatalf("bucket 0.001:\n%s", out)
	}
	if !strings.Contains(out, `latency_bucket{endpoint="e",le="+Inf"} 2`) {
		t.Fatalf("inf bucket:\n%s", out)
	}
	if !strings.Contains(out, `latency_count{endpoint="e"} 2`) {
		t.Fatalf("count:\n%s", out)
	}
}

func TestLabelEscaping(t *testing.T) {
	r := New()
	r.Inc("m", "M.", map[string]string{"reason": `a"b`}, 1)
	if !strings.Contains(snap(t, r), `reason="a\"b"`) {
		t.Fatalf("not escaped: %s", snap(t, r))
	}
}

func TestObserver(t *testing.T) {
	r := New()
	o := NewObserver(r)
	o.Bundle("bundle-1", "sha256:abc", "1.0")
	o.Record(engine.Decision{
		EndpointID: "get-account", Allowed: false, Status: 503,
		Reason: "stale_attributes", ResourceType: "account",
		PIPCache: "miss", PIPLagSeconds: 42,
		Violations: []dq.Violation{{RuleID: "email", Blocking: false}},
	})
	out := snap(t, r)
	for _, want := range []string{
		`controls_decisions_total{decision="deny",endpoint="get-account",reason="stale_attributes"} 1`,
		`controls_stale_denials_total{endpoint="get-account"} 1`,
		`controls_pip_lag_seconds{resource_type="account"} 42`,
		`controls_dq_violations_total{blocking="false",resource_type="account",rule="email"} 1`,
		`controls_bundle_info{bundle_digest="sha256:abc",bundle_version="bundle-1",schema_version="1.0"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}
