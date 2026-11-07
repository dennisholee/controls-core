package policy

import (
	"testing"

	"example.com/controls/core/internal/config"
)

func evaluator() *RuleEvaluator {
	b := &config.Bundle{Rules: config.Rules{
		DefaultEffect: "deny",
		Rules: []config.Rule{
			{
				ID: "deny.sanctioned", Effect: "deny", ResourceType: "*", Action: "*",
				When: config.Expr{Field: "subject.claims.status", Op: "eq", Value: "sanctioned"},
			},
			{
				ID: "allow.owner", Effect: "allow", ResourceType: "account", Action: "read",
				When: config.Expr{Field: "subject.sub", Op: "eq", Value: "$resource.ownerId"},
			},
		},
	}}
	return New(b)
}

func ctx(sub string, claims map[string]any, owner string) Context {
	return Context{
		Subject:  map[string]any{"sub": sub, "claims": claims},
		Resource: map[string]any{"ownerId": owner},
		Request:  map[string]any{"method": "GET"},
	}
}

func TestDenyRuleWins(t *testing.T) {
	res := evaluator().Decide("account", "read", ctx("u-1", map[string]any{"status": "sanctioned"}, "u-1"))
	if res.Allowed {
		t.Fatalf("deny rule must win: %+v", res)
	}
}

func TestAllowRule(t *testing.T) {
	res := evaluator().Decide("account", "read", ctx("u-1", nil, "u-1"))
	if !res.Allowed || res.Reason != "allow:allow.owner" {
		t.Fatalf("got %+v", res)
	}
}

func TestDefaultDeny(t *testing.T) {
	res := evaluator().Decide("account", "read", ctx("u-2", nil, "u-1"))
	if res.Allowed {
		t.Fatalf("expected default deny: %+v", res)
	}
}

func TestWrongActionDoesNotMatch(t *testing.T) {
	res := evaluator().Decide("account", "write", ctx("u-1", nil, "u-1"))
	if res.Allowed {
		t.Fatalf("expected default deny: %+v", res)
	}
}

func TestStrictTypeComparison(t *testing.T) {
	if equal("5000", float64(5000)) {
		t.Fatal("string must not equal number")
	}
	if equal("true", true) {
		t.Fatal("string must not equal bool")
	}
	if !equal("5000", "5000") || !equal(true, true) || !equal(nil, nil) {
		t.Fatal("same-type equality failed")
	}
	if _, ok := toFloat("5000"); ok {
		t.Fatal("strings must not be parsed as numbers")
	}
}
