package config

import (
	"strings"
	"testing"
)

func loadTestBundle(t *testing.T) *Bundle {
	t.Helper()
	b, err := LoadDir("../../integration/testdata/config")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return b
}

func TestLoadValidBundle(t *testing.T) {
	b := loadTestBundle(t)
	if len(b.Registry.Endpoints) != 2 {
		t.Fatalf("endpoints = %d", len(b.Registry.Endpoints))
	}
	if b.Rules.DefaultEffect != "deny" {
		t.Fatalf("default effect = %q", b.Rules.DefaultEffect)
	}
}

func TestLockedFieldCannotBeRelaxed(t *testing.T) {
	b := loadTestBundle(t)
	p := b.Masks.Policies["journey.account.read"]
	p.Fields["ssn"] = MaskField{Mode: "partial", KeepSuffix: 4}
	b.Masks.Policies["journey.account.read"] = p
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected locked-field error, got %v", err)
	}
}

func TestEndpointRequiresKnownMaskPolicy(t *testing.T) {
	b := loadTestBundle(t)
	b.Registry.Endpoints[0].MaskPolicy = "missing.policy"
	err := b.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown mask policy") {
		t.Fatalf("expected unknown policy error, got %v", err)
	}
}

func TestEndpointMatch(t *testing.T) {
	b := loadTestBundle(t)
	ep, params := b.EndpointFor("GET", "/accounts/A123")
	if ep == nil || ep.ID != "get-account" {
		t.Fatalf("no match: %+v", ep)
	}
	if params["accountId"] != "A123" {
		t.Fatalf("params = %v", params)
	}
	if ep, _ := b.EndpointFor("GET", "/accounts/A123/extra"); ep != nil {
		t.Fatalf("should not match extra segment")
	}
	if ep, _ := b.EndpointFor("POST", "/accounts/A123"); ep != nil {
		t.Fatalf("method must match")
	}
}
