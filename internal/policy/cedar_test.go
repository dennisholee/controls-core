package policy

import "testing"

const cedarSource = `
permit (
  principal,
  action == Action::"read",
  resource
)
when {
  principal.sub == resource.ownerId
};
`

func cedarCtx(sub, owner string) Context {
	return Context{
		Subject:    map[string]any{"sub": sub, "claims": map[string]any{"region": "US"}},
		Resource:   map[string]any{"ownerId": owner, "region": "US"},
		ResourceID: "A123",
	}
}

func TestCedarAllowAndDeny(t *testing.T) {
	ev, err := NewCedarEvaluator(cedarSource)
	if err != nil {
		t.Fatal(err)
	}
	if res := ev.Decide("account", "read", cedarCtx("u-1", "u-1")); !res.Allowed {
		t.Fatalf("expected allow: %+v", res)
	}
	if res := ev.Decide("account", "read", cedarCtx("u-2", "u-1")); res.Allowed {
		t.Fatalf("expected deny: %+v", res)
	}
}

func TestCedarRejectsInvalidPolicy(t *testing.T) {
	if _, err := NewCedarEvaluator("this is not cedar"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestEvaluatorSelection(t *testing.T) {
	if _, err := NewCedarEvaluator(cedarSource); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkCedarDecide(b *testing.B) {
	ev, err := NewCedarEvaluator(cedarSource)
	if err != nil {
		b.Fatal(err)
	}
	ctx := cedarCtx("u-1", "u-1")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if res := ev.Decide("account", "read", ctx); !res.Allowed {
			b.Fatal("unexpected deny")
		}
	}
}

func TestCedarSeesRequestContext(t *testing.T) {
	ev, err := NewCedarEvaluator(`
permit (principal, action, resource)
when { context.method == "GET" };
`)
	if err != nil {
		t.Fatal(err)
	}
	c := cedarCtx("u-1", "u-1")
	c.Request = map[string]any{"method": "GET", "path": "/accounts/A123"}
	if res := ev.Decide("account", "read", c); !res.Allowed {
		t.Fatalf("expected allow: %+v", res)
	}
	c.Request["method"] = "POST"
	if res := ev.Decide("account", "read", c); res.Allowed {
		t.Fatalf("expected deny: %+v", res)
	}
}
