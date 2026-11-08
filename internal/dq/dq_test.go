package dq

import (
	"testing"

	"example.com/controls/core/internal/config"
)

func bundle(rules ...config.DQRule) *config.Bundle {
	for i := range rules {
		rules[i].ResourceType = "*"
	}
	return &config.Bundle{DQ: config.DQConfig{Rules: rules}}
}

func TestOperators(t *testing.T) {
	e, err := New(bundle(
		config.DQRule{ID: "req", Field: "name", Required: true},
		config.DQRule{ID: "pattern", Field: "code", Op: "pattern", Pattern: `^[A-Z]{3}$`},
		config.DQRule{ID: "max", Field: "name", Op: "maxLength", MaxLength: 4},
		config.DQRule{ID: "type", Field: "amount", Op: "type", Value: "number"},
		config.DQRule{ID: "eq", Field: "currency", Op: "eq", Value: "USD"},
		config.DQRule{ID: "neq", Field: "status", Op: "neq", Value: "closed"},
		config.DQRule{ID: "gt", Field: "amount", Op: "gt", Value: 0},
		config.DQRule{ID: "gte", Field: "amount", Op: "gte", Value: 1},
		config.DQRule{ID: "lt", Field: "amount", Op: "lt", Value: 100},
		config.DQRule{ID: "lte", Field: "amount", Op: "lte", Value: 99},
	))
	if err != nil {
		t.Fatal(err)
	}
	ok := map[string]any{"name": "abc", "code": "ABC", "amount": 50, "currency": "USD", "status": "open"}
	if v := e.Check("payment", "write", "", ok); len(v) != 0 {
		t.Fatalf("unexpected violations: %+v", v)
	}
	bad := map[string]any{"name": "abcdef", "code": "ab", "amount": 0, "currency": "EUR", "status": "closed"}
	if v := e.Check("payment", "write", "", bad); len(v) != 6 {
		t.Fatalf("violations = %d, want 6: %+v", len(v), v)
	}
}

func TestInvalidRulesFailAtLoad(t *testing.T) {
	cases := []config.DQRule{
		{ID: "op", Field: "x", Op: "greaterThan"},
		{ID: "pattern", Field: "x", Op: "pattern", Pattern: "("},
		{ID: "type", Field: "x", Op: "type", Value: "strng"},
		{ID: "action", Field: "x", Action: "upsert"},
	}
	for _, r := range cases {
		if _, err := New(bundle(r)); err == nil {
			t.Fatalf("expected load error for %+v", r)
		}
	}
}

func TestActionScoping(t *testing.T) {
	e, err := New(bundle(config.DQRule{
		ID: "first-name", Field: "legalName.firstName", Required: true, Action: "write",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if v := e.Check("party", "read", "", map[string]any{}); len(v) != 0 {
		t.Fatalf("write-scoped rule ran on read: %+v", v)
	}
	if v := e.Check("party", "write", "", map[string]any{}); len(v) != 1 {
		t.Fatalf("write-scoped rule did not run on write: %+v", v)
	}
	if v := e.Check("party", "delete", "", map[string]any{}); len(v) != 0 {
		t.Fatalf("write-scoped rule ran on delete: %+v", v)
	}
}

func TestWildcardPaths(t *testing.T) {
	e, err := New(bundle(
		config.DQRule{ID: "country", Field: "addresses[*].country", Op: "pattern", Pattern: `^[A-Z]{2}$`},
		config.DQRule{ID: "country-required", Field: "addresses[*].country", Required: true},
	))
	if err != nil {
		t.Fatal(err)
	}
	ok := map[string]any{"addresses": []any{
		map[string]any{"country": "US"},
		map[string]any{"country": "CA"},
	}}
	if v := e.Check("party", "write", "", ok); len(v) != 0 {
		t.Fatalf("unexpected violations: %+v", v)
	}
	bad := map[string]any{"addresses": []any{
		map[string]any{"country": "US"},
		map[string]any{"country": "USA"},
	}}
	if v := e.Check("party", "write", "", bad); len(v) != 1 {
		t.Fatalf("violations = %d, want 1: %+v", len(v), v)
	}
	empty := map[string]any{"addresses": []any{}}
	if v := e.Check("party", "write", "", empty); len(v) != 1 {
		t.Fatalf("empty array should fail required: %+v", v)
	}
}

func TestEndpointScoping(t *testing.T) {
	e, err := New(bundle(config.DQRule{
		ID: "merge-only", ResourceType: "party", Action: "write",
		Endpoint: "merge-party", Field: "sourcePartyId", Required: true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if v := e.Check("party", "write", "update-party", map[string]any{}); len(v) != 0 {
		t.Fatalf("endpoint-scoped rule ran on another endpoint: %+v", v)
	}
	if v := e.Check("party", "write", "merge-party", map[string]any{}); len(v) != 1 {
		t.Fatalf("endpoint-scoped rule did not run: %+v", v)
	}
}
