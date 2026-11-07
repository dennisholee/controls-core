package engine

import (
	"context"
	"testing"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/pipclient"
)

type fakePIP struct {
	lag          int
	resolveCalls int
	freshCalls   int
}

func (f *fakePIP) Resolve(_ context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	f.resolveCalls++
	return f.attrs(refs), nil
}

func (f *fakePIP) ResolveFresh(_ context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	f.freshCalls++
	return f.attrs(refs), nil
}

func (f *fakePIP) Classification(context.Context, string) (map[string]string, error) {
	return nil, nil
}

func (f *fakePIP) attrs(refs []pipclient.Ref) map[string]pipclient.Attribute {
	out := map[string]pipclient.Attribute{}
	for _, r := range refs {
		out[r.Type+"|"+r.ID] = pipclient.Attribute{
			Type: r.Type, ID: r.ID, Found: true,
			Attributes: map[string]any{"ownerId": "u-1", "status": "active"},
			LagSeconds: f.lag,
			Cache:      "miss",
		}
	}
	return out
}

func freshnessBundle(freshness string, maxStale int) *config.Bundle {
	b := &config.Bundle{}
	b.Registry.Endpoints = []config.Endpoint{{
		ID:                  "get-account",
		Method:              "GET",
		Path:                "/accounts/{accountId}",
		ResourceType:        "account",
		Action:              "read",
		ResourceID:          config.Ref{From: "path", Name: "accountId"},
		ResourceFrom:        "pip",
		Attributes:          []string{"ownerId", "status"},
		Freshness:           freshness,
		MaxStalenessSeconds: maxStale,
	}}
	b.Rules.DefaultEffect = "allow"
	b.Masks.Policies = map[string]config.MaskPolicy{}
	return b
}

func run(b *config.Bundle, pip PIP) Result {
	e := New(b, pip)
	return e.ProcessRequest(context.Background(), Request{
		Method: "GET", Path: "/accounts/A123",
		Subject: Subject{Sub: "u-1"},
	})
}

func TestCachedIgnoresLag(t *testing.T) {
	pip := &fakePIP{lag: 30}
	res := run(freshnessBundle("cached", 0), pip)
	if !res.Allowed {
		t.Fatalf("cached endpoint should allow: %+v", res)
	}
	if pip.resolveCalls != 1 || pip.freshCalls != 0 {
		t.Fatalf("unexpected calls: resolve=%d fresh=%d", pip.resolveCalls, pip.freshCalls)
	}
}

func TestFreshBypassesCache(t *testing.T) {
	pip := &fakePIP{lag: 0}
	res := run(freshnessBundle("fresh", 0), pip)
	if !res.Allowed {
		t.Fatalf("fresh endpoint should allow: %+v", res)
	}
	if pip.freshCalls != 1 {
		t.Fatalf("fresh calls = %d", pip.freshCalls)
	}
}

func TestFreshDeniesStale(t *testing.T) {
	res := run(freshnessBundle("fresh", 0), &fakePIP{lag: 30})
	if res.Allowed || res.Status != 503 || res.Reason != "stale_attributes" {
		t.Fatalf("expected stale deny, got %+v", res)
	}
}

func TestDenyOnStaleHonoursBound(t *testing.T) {
	if res := run(freshnessBundle("deny-on-stale", 10), &fakePIP{lag: 5}); !res.Allowed {
		t.Fatalf("lag within bound should allow: %+v", res)
	}
	res := run(freshnessBundle("deny-on-stale", 10), &fakePIP{lag: 11})
	if res.Allowed || res.Reason != "stale_attributes" {
		t.Fatalf("lag over bound should deny: %+v", res)
	}
}

func TestFreshWithoutFreshClientFallsBack(t *testing.T) {
	pip := &plainPIP{lag: 0}
	res := run(freshnessBundle("fresh", 0), pip)
	if !res.Allowed || pip.calls != 1 {
		t.Fatalf("fallback failed: %+v calls=%d", res, pip.calls)
	}
}

type plainPIP struct{ lag, calls int }

func (p *plainPIP) Resolve(_ context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	p.calls++
	out := map[string]pipclient.Attribute{}
	for _, r := range refs {
		out[r.Type+"|"+r.ID] = pipclient.Attribute{
			Type: r.Type, ID: r.ID, Found: true,
			Attributes: map[string]any{"ownerId": "u-1"},
			LagSeconds: p.lag,
		}
	}
	return out, nil
}

func (p *plainPIP) Classification(context.Context, string) (map[string]string, error) {
	return nil, nil
}

func TestDecisionHook(t *testing.T) {
	var got Decision
	e := New(freshnessBundle("cached", 0), &fakePIP{lag: 0})
	e.SetHooks(Hooks{OnDecision: func(d Decision) { got = d }})
	res := e.ProcessRequest(context.Background(), Request{
		RequestID: "req-9", Method: "GET", Path: "/accounts/A123",
		Subject: Subject{Sub: "u-1"},
	})
	if !res.Allowed {
		t.Fatalf("expected allow: %+v", res)
	}
	if got.RequestID != "req-9" || got.EndpointID != "get-account" ||
		got.Subject != "u-1" || got.Action != "read" || !got.Allowed ||
		got.PIPCache != "miss" || got.Latency <= 0 {
		t.Fatalf("unexpected decision event: %+v", got)
	}
}
