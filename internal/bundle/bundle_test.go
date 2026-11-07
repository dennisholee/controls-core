package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sampleSrc(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry", "accounts.json"), `{
	  "version": "1.0",
	  "defaultUnregistered": "deny",
	  "endpoints": [{
	    "id": "get-account", "service": "accounts", "method": "GET",
	    "path": "/accounts/{accountId}", "resourceType": "account",
	    "resourceId": {"from": "path", "name": "accountId"},
	    "resourceSource": "pip", "maskPolicy": "read"
	  }]
	}`)
	writeFile(t, filepath.Join(dir, "policies", "rules.json"), `{
	  "version": "1.0", "defaultEffect": "deny",
	  "rules": [
	    {"id": "allow-all", "effect": "allow", "resourceType": "account", "action": "read",
	     "when": {"field": "subject.sub", "op": "eq", "value": "u-1"}},
	    {"id": "deny-all", "effect": "deny", "resourceType": "account", "action": "read",
	     "when": {"field": "subject.claims.status", "op": "eq", "value": "blocked"}}
	  ]
	}`)
	writeFile(t, filepath.Join(dir, "mask-policies", "masks.json"), `{
	  "version": "1.0",
	  "policies": {"read": {"default": "none", "fields": {"ssn": {"mode": "full"}}}}
	}`)
	writeFile(t, filepath.Join(dir, "dq", "dq.json"), `{
	  "version": "1.0",
	  "rules": [{"id": "amount-positive", "resourceType": "payment", "field": "amount", "op": "gt", "value": 0}]
	}`)
	return dir
}

func TestRenderValidatesAndIsDeterministic(t *testing.T) {
	src := sampleSrc(t)
	out1 := t.TempDir()
	res1, err := Render(Options{Src: src, Out: out1, Service: "accounts"})
	if err != nil {
		t.Fatal(err)
	}
	if res1.Digest == "" || res1.Counts.Endpoints != 1 || res1.Counts.Rules != 2 {
		t.Fatalf("unexpected result: %+v", res1)
	}
	out2 := t.TempDir()
	res2, err := Render(Options{Src: src, Out: out2, Service: "accounts"})
	if err != nil {
		t.Fatal(err)
	}
	if res1.Digest != res2.Digest {
		t.Fatalf("digest not deterministic: %s vs %s", res1.Digest, res2.Digest)
	}
	for _, f := range []string{"rules.json", "registry.json", "mask-policies.json", "dq.json", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(out1, f)); err != nil {
			t.Fatalf("missing %s: %v", f, err)
		}
	}
}

func TestOverlayDisablesAndOverrides(t *testing.T) {
	src := sampleSrc(t)
	writeFile(t, filepath.Join(src, "env", "prod", "overrides.json"), `{
	  "environment": "prod",
	  "bundle": {"disableRules": ["deny-all"], "defaultUnregistered": "deny"}
	}`)
	out := t.TempDir()
	res, err := Render(Options{Src: src, Out: out, Env: "prod", Service: "accounts"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Counts.Rules != 1 {
		t.Fatalf("rules = %d", res.Counts.Rules)
	}
}

func TestUnknownDisableIDFails(t *testing.T) {
	src := sampleSrc(t)
	writeFile(t, filepath.Join(src, "env", "prod", "overrides.json"), `{
	  "environment": "prod", "bundle": {"disableRules": ["typo"]}
	}`)
	if _, err := Render(Options{Src: src, Env: "prod", Check: true}); err == nil {
		t.Fatal("expected unknown rule id error")
	}
}

func TestServiceFilterSelectsOnlyOwnEndpoints(t *testing.T) {
	src := sampleSrc(t)
	writeFile(t, filepath.Join(src, "registry", "payments.json"), `{
	  "version": "1.0", "defaultUnregistered": "deny",
	  "endpoints": [{
	    "id": "create-payment", "service": "payments", "method": "POST", "path": "/payments",
	    "resourceType": "payment", "resourceId": {"from": "body", "path": "$._id"},
	    "resourceSource": "body", "selfDescribing": true
	  }]
	}`)
	out := t.TempDir()
	res, err := Render(Options{Src: src, Out: out, Service: "payments"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Counts.Endpoints != 1 {
		t.Fatalf("endpoints = %d", res.Counts.Endpoints)
	}
	var reg struct {
		Endpoints []struct {
			ID string `json:"id"`
		} `json:"endpoints"`
	}
	raw, _ := os.ReadFile(filepath.Join(out, "registry.json"))
	if err := json.Unmarshal(raw, &reg); err != nil {
		t.Fatal(err)
	}
	if reg.Endpoints[0].ID != "create-payment" {
		t.Fatalf("got %s", reg.Endpoints[0].ID)
	}
}

func TestOverlayRejectsAllowUnregistered(t *testing.T) {
	src := sampleSrc(t)
	writeFile(t, filepath.Join(src, "env", "prod", "overrides.json"), `{
	  "environment": "prod", "bundle": {"defaultUnregistered": "allow"}
	}`)
	if _, err := Render(Options{Src: src, Env: "prod", Check: true}); err == nil {
		t.Fatal("expected overlay allow to be rejected")
	}
}

func TestOverlayRejectsUnknownKeys(t *testing.T) {
	src := sampleSrc(t)
	writeFile(t, filepath.Join(src, "env", "prod", "overrides.json"), `{
	  "environment": "prod", "bundle": {"disableRule": ["x"]}
	}`)
	if _, err := Render(Options{Src: src, Env: "prod", Check: true}); err == nil {
		t.Fatal("expected unknown overlay key to be rejected")
	}
}
