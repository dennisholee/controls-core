package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/policy"
)

func TestOPAEngine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input struct {
				Subject map[string]any `json:"subject"`
			} `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": body.Input.Subject["sub"] == "u-1"})
	}))
	defer srv.Close()

	b := freshnessBundle("cached", 0)
	b.Rules.Engine = "opa"
	b.Rules.Rules = nil
	b.Rules.OPA = &config.OPAConfig{Decision: "controls/authz/allow"}
	e := NewWithOptions(b, &fakePIP{}, Options{Policy: policy.Options{
		OPAURL: srv.URL, OPATimeout: 200 * time.Millisecond,
	}})
	if err := e.StartupError(); err != nil {
		t.Fatal(err)
	}
	if res := e.ProcessRequest(context.Background(), Request{
		Method: "GET", Path: "/accounts/A123", Subject: Subject{Sub: "u-1"},
	}); !res.Allowed || res.Reason != "opa:allow" {
		t.Fatalf("expected opa allow: %+v", res)
	}
	if res := e.ProcessRequest(context.Background(), Request{
		Method: "GET", Path: "/accounts/A123", Subject: Subject{Sub: "u-2"},
	}); res.Allowed {
		t.Fatalf("expected opa deny: %+v", res)
	}
}

func TestOPAEngineWithoutURLFailsStartup(t *testing.T) {
	b := freshnessBundle("cached", 0)
	b.Rules.Engine = "opa"
	b.Rules.Rules = nil
	b.Rules.OPA = &config.OPAConfig{Decision: "controls/authz/allow"}
	e := New(b, &fakePIP{})
	if e.StartupError() == nil {
		t.Fatal("expected startup error without OPA URL")
	}
}
