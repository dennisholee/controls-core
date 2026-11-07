package config

import (
	"os"
	"path/filepath"
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

func TestManifestLoadedWhenPresent(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"registry.json", "rules.json", "mask-policies.json", "dq.json"} {
		raw, err := os.ReadFile(filepath.Join("../../integration/testdata/config", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"schemaVersion":"1.0","bundleDigest":"sha256:abc","bundleVersion":"bundle-abc"}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Manifest == nil || b.Manifest.BundleVersion != "bundle-abc" || b.Manifest.BundleDigest != "sha256:abc" {
		t.Fatalf("manifest = %+v", b.Manifest)
	}
}

func TestMaskErrorsDefaultsTrue(t *testing.T) {
	var ep Endpoint
	if !ep.MaskErrorsEnabled() {
		t.Fatal("maskErrors must default to true")
	}
	f := false
	ep.MaskErrors = &f
	if ep.MaskErrorsEnabled() {
		t.Fatal("explicit false must be honoured")
	}
}

func TestAllowUnregisteredRejected(t *testing.T) {
	b := loadTestBundle(t)
	b.Registry.DefaultUnregistered = "allow"
	if err := b.Validate(); err == nil {
		t.Fatal("defaultUnregistered allow must be rejected")
	}
}

func TestDuplicateRouteRejected(t *testing.T) {
	b := loadTestBundle(t)
	b.Registry.Endpoints = append(b.Registry.Endpoints, b.Registry.Endpoints[0])
	b.Registry.Endpoints[len(b.Registry.Endpoints)-1].ID = "dup"
	if err := b.Validate(); err == nil {
		t.Fatal("duplicate method+path must be rejected")
	}
}
