package testapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/conformance"
	"example.com/controls/core/internal/pipclient"
)

func newServer(t *testing.T, opts ...Option) *httptest.Server {
	t.Helper()
	bundle, err := config.LoadDir("../../integration/testdata/config")
	if err != nil {
		t.Fatalf("load bundle: %v", err)
	}
	pip, err := NewLocalPIP("../../pip/fixtures/attributes.json")
	if err != nil {
		t.Fatalf("load PIP fixtures: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := New(bundle, pip, log, append([]Option{WithPIPMode("fixtures")}, opts...)...)
	if err := api.StartupError(); err != nil {
		t.Fatalf("startup: %v", err)
	}
	srv := httptest.NewServer(api.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, srv *httptest.Server, path, body string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp, raw
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

func TestProcessAllowsAndMasks(t *testing.T) {
	srv := newServer(t)
	body := `{
	  "request": {"method": "GET", "path": "/accounts/A123",
	              "headers": {"x-subject": "u-1", "x-claim-region": "US"}},
	  "response": {"status": 200, "body": {
	    "accountNumber": "1234567890",
	    "holder": {"email": "jane.doe@example.com"},
	    "ssn": "111-22-3333"
	  }}
	}`
	resp, raw := postJSON(t, srv, "/v1/process", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, raw)
	}
	if bytes.Contains(raw, []byte("jane.doe@example.com")) {
		t.Fatalf("raw response leaked the unmasked email: %s", raw)
	}
	out := decode[ProcessResult](t, raw)
	if !out.Decision.Allowed || out.Decision.Status != 200 {
		t.Fatalf("decision = %+v", out.Decision)
	}
	if out.Decision.EndpointID != "get-account" || out.Decision.ResourceType != "account" {
		t.Fatalf("binding = %+v", out.Decision)
	}
	if out.Response == nil {
		t.Fatal("expected a masked response")
	}
	var masked map[string]any
	if err := json.Unmarshal(out.Response.Body, &masked); err != nil {
		t.Fatal(err)
	}
	if masked["accountNumber"] != "******7890" {
		t.Fatalf("accountNumber = %v", masked["accountNumber"])
	}
	if masked["ssn"] != "[REDACTED]" {
		t.Fatalf("ssn = %v", masked["ssn"])
	}
	holder := masked["holder"].(map[string]any)
	if holder["email"] != "j***@example.com" {
		t.Fatalf("email = %v", holder["email"])
	}
}

func TestProcessDeniesWithoutResponse(t *testing.T) {
	srv := newServer(t)
	resp, raw := postJSON(t, srv, "/v1/process", `{
	  "request": {"method": "GET", "path": "/accounts/A123",
	              "headers": {"x-subject": "u-9"}},
	  "response": {"status": 200, "body": {"ssn": "111-22-3333"}}
	}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, raw)
	}
	out := decode[ProcessResult](t, raw)
	if out.Decision.Allowed || out.Decision.Status != http.StatusForbidden {
		t.Fatalf("decision = %+v", out.Decision)
	}
	if out.Decision.Reason != "default:deny(no-allow-match)" {
		t.Fatalf("reason = %q", out.Decision.Reason)
	}
	if out.Response != nil {
		t.Fatalf("denied request returned a body: %+v", out.Response)
	}
}

func TestProcessSubjectObjectAndQueryString(t *testing.T) {
	srv := newServer(t)
	_, raw := postJSON(t, srv, "/v1/process", `{
	  "request": {"method": "GET", "path": "/accounts/A123?view=full"},
	  "subject": {"sub": "u-1", "claims": {"region": "US"}}
	}`)
	out := decode[ProcessResult](t, raw)
	if !out.Decision.Allowed {
		t.Fatalf("top-level subject was not used: %+v", out.Decision)
	}
}

func TestProcessValidation(t *testing.T) {
	srv := newServer(t)
	cases := []struct {
		name string
		body string
		code string
	}{
		{"missing path", `{"request":{"method":"GET"}}`, "invalid_request"},
		{"path without slash", `{"request":{"method":"GET","path":"accounts/A123"}}`, "invalid_request"},
		{"invalid json", `{"request":`, "invalid_json"},
		{"trailing data", `{"request":{"method":"GET","path":"/x"}} {}`, "invalid_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, raw := postJSON(t, srv, "/v1/process", tc.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d: %s", resp.StatusCode, raw)
			}
			var e map[string]string
			if err := json.Unmarshal(raw, &e); err != nil {
				t.Fatal(err)
			}
			if e["error"] != tc.code {
				t.Fatalf("error = %q, want %q", e["error"], tc.code)
			}
		})
	}
}

func TestProcessBodyLimit(t *testing.T) {
	srv := newServer(t, WithMaxBodyBytes(64))
	resp, raw := postJSON(t, srv, "/v1/process", `{"request":{"method":"GET","path":"/accounts/A123","headers":{"x-subject":"u-1"}}}`)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d: %s", resp.StatusCode, raw)
	}
}

func TestEndpointsDiscovery(t *testing.T) {
	srv := newServer(t)
	resp, err := http.Get(srv.URL + "/v1/endpoints")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var doc endpointsResponse
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.DefaultUnregistered != "deny" || len(doc.Endpoints) != 2 {
		t.Fatalf("registry = %+v", doc)
	}
	var found bool
	for _, ep := range doc.Endpoints {
		if ep.ID != "get-account" {
			continue
		}
		found = true
		if ep.Path != "/accounts/{accountId}" || ep.MaskPolicy != "journey.account.read" {
			t.Fatalf("endpoint = %+v", ep)
		}
		if ep.Mask == nil || ep.Mask.Fields["accountNumber"].Mode != "partial" {
			t.Fatalf("mask = %+v", ep.Mask)
		}
	}
	if !found {
		t.Fatal("get-account missing")
	}
}

func TestInfo(t *testing.T) {
	srv := newServer(t, WithBundleDir("/tmp/bundle"))
	resp, err := http.Get(srv.URL + "/v1/info")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var info infoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.API != "controls-test-api" || info.PIP.Mode != "fixtures" {
		t.Fatalf("info = %+v", info)
	}
	if info.Bundle.Endpoints != 2 || info.Bundle.Rules != 3 || info.Bundle.MaskPolicies != 2 || info.Bundle.DQRules != 3 {
		t.Fatalf("counts = %+v", info.Bundle)
	}
	if info.Bundle.PolicyEngine != "rules" {
		t.Fatalf("engine = %q", info.Bundle.PolicyEngine)
	}
}

func TestFixturePass(t *testing.T) {
	srv := newServer(t)
	fixture := `{
	  "name": "account-owner-masked",
	  "pip": {"account": {"A123": {"ownerId": "u-1", "region": "US", "status": "active"}}},
	  "request": {"method": "GET", "path": "/accounts/A123",
	              "headers": {"x-subject": "u-1", "x-claim-region": "US"}},
	  "response": {"status": 200, "body": {
	    "accountNumber": "1234567890",
	    "holder": {"email": "jane.doe@example.com"},
	    "ssn": "111-22-3333"
	  }},
	  "expect": {"allowed": true, "status": 200, "reason": "allow:account.read.owner-or-region",
	             "masked": {"accountNumber": "******7890", "holder.email": "j***@example.com"}}
	}`
	resp, raw := postJSON(t, srv, "/v1/test", fixture)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, raw)
	}
	ev := decode[struct {
		Pass   bool                `json:"pass"`
		Checks []conformance.Check `json:"checks"`
	}](t, raw)
	if !ev.Pass {
		t.Fatalf("fixture failed: %s", raw)
	}
	for _, c := range ev.Checks {
		if !c.OK {
			t.Fatalf("check failed: %+v", c)
		}
	}
}

func TestFixtureFailReturnsChecks(t *testing.T) {
	srv := newServer(t)
	fixture := `{
	  "name": "wrong",
	  "request": {"method": "GET", "path": "/accounts/A123",
	              "headers": {"x-subject": "u-1", "x-claim-region": "US"}},
	  "expect": {"allowed": false}
	}`
	resp, raw := postJSON(t, srv, "/v1/test", fixture)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, raw)
	}
	ev := decode[struct {
		Pass   bool                `json:"pass"`
		Checks []conformance.Check `json:"checks"`
	}](t, raw)
	if ev.Pass {
		t.Fatalf("expected failure: %s", raw)
	}
	if len(ev.Checks) == 0 || ev.Checks[0].Check != "allowed" || ev.Checks[0].OK {
		t.Fatalf("checks = %+v", ev.Checks)
	}
}

func TestFixtureUsesRunningPIPWhenNoOverride(t *testing.T) {
	srv := newServer(t)
	fixture := `{
	  "name": "fallback-pip",
	  "request": {"method": "GET", "path": "/accounts/A123",
	              "headers": {"x-subject": "u-1"}}
	}`
	_, raw := postJSON(t, srv, "/v1/test", fixture)
	ev := decode[struct {
		Decision struct {
			Allowed bool `json:"allowed"`
		} `json:"decision"`
	}](t, raw)
	if !ev.Decision.Allowed {
		t.Fatalf("running PIP was not used: %s", raw)
	}
}

func TestSpecAndDocs(t *testing.T) {
	srv := newServer(t)
	spec, err := http.Get(srv.URL + "/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer spec.Body.Close()
	raw, _ := io.ReadAll(spec.Body)
	if !strings.Contains(string(raw), "openapi: 3.0.3") || !strings.Contains(string(raw), "Controls Test API") {
		t.Fatalf("unexpected spec: %s", raw)
	}

	docs, err := http.Get(srv.URL + "/docs")
	if err != nil {
		t.Fatal(err)
	}
	defer docs.Body.Close()
	html, _ := io.ReadAll(docs.Body)
	if !strings.Contains(string(html), "Controls Test API") || !strings.Contains(string(html), "/v1/process") {
		t.Fatalf("unexpected console: %s", html)
	}

	root, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Body.Close()
	if root.StatusCode != http.StatusOK || root.Request.URL.Path != "/docs" {
		t.Fatalf("root = %d %s", root.StatusCode, root.Request.URL.Path)
	}
}

func TestHealth(t *testing.T) {
	srv := newServer(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d", path, resp.StatusCode)
		}
	}
}

func TestLocalPIP(t *testing.T) {
	pip, err := NewLocalPIP("../../pip/fixtures/attributes.json")
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := pip.Resolve(t.Context(), []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId", "region"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := attrs["account|A123"]
	if !ok || !a.Found {
		t.Fatalf("attribute = %+v", a)
	}
	if a.Attributes["ownerId"] != "u-1" || len(a.Attributes) != 2 {
		t.Fatalf("projection = %+v", a.Attributes)
	}
	classes, err := pip.Classification(t.Context(), "account")
	if err != nil {
		t.Fatal(err)
	}
	if classes["ssn"] != "pii.locked" {
		t.Fatalf("classification = %+v", classes)
	}
}
