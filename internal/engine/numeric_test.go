package engine

import (
	"context"
	"strings"
	"testing"

	"example.com/controls/core/internal/config"
)

func TestMaskPreservesNumericLiterals(t *testing.T) {
	b := &config.Bundle{
		Registry: config.Registry{Endpoints: []config.Endpoint{{
			ID: "get", Method: "GET", Path: "/x", ResourceType: "account", MaskPolicy: "p",
		}}},
		Masks: config.MaskPolicies{Policies: map[string]config.MaskPolicy{
			"p": {Fields: map[string]config.MaskField{"secret": {Mode: "full"}}},
		}},
	}
	e := New(b, nil)
	ep := e.Endpoint("GET", "/x")
	if ep == nil {
		t.Fatal("endpoint missing")
	}
	out, err := e.MaskResponseCtx(context.Background(), ep,
		[]byte(`{"accountNumber":123456789012345678,"amount":120.50,"secret":"x"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, `123456789012345678`) {
		t.Fatalf("large integer changed: %s", got)
	}
	if !strings.Contains(got, `120.50`) {
		t.Fatalf("decimal representation changed: %s", got)
	}
	if strings.Contains(got, `"secret":"x"`) {
		t.Fatalf("secret not masked: %s", got)
	}
}
