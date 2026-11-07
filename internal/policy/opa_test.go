package policy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOPAAllowAndDeny(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/data/controls/authz/allow" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body struct {
			Input struct {
				Subject map[string]any `json:"subject"`
				Action  string         `json:"action"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		allow := body.Input.Subject["sub"] == "u-1" && body.Input.Action == "read"
		_ = json.NewEncoder(w).Encode(map[string]any{"result": allow})
	}))
	defer srv.Close()

	ev, err := NewOPAEvaluator(srv.URL, "controls/authz/allow", 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if res := ev.Decide("account", "read", cedarCtx("u-1", "u-1")); !res.Allowed || res.Reason != "opa:allow" {
		t.Fatalf("expected allow: %+v", res)
	}
	if res := ev.Decide("account", "read", cedarCtx("u-2", "u-1")); res.Allowed {
		t.Fatalf("expected deny: %+v", res)
	}
}

func TestOPAFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	ev, err := NewOPAEvaluator(srv.URL, "controls/authz/allow", 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if res := ev.Decide("account", "read", cedarCtx("u-1", "u-1")); res.Allowed || res.Reason != "opa:error" {
		t.Fatalf("expected fail-closed: %+v", res)
	}
}

func TestOPARequiresURL(t *testing.T) {
	if _, err := NewOPAEvaluator("", "controls/authz/allow", 0); err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestOPAHonoursRequestContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
	}))
	defer srv.Close()
	ev, err := NewOPAEvaluator(srv.URL, "controls/authz/allow", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := cedarCtx("u-1", "u-1")
	c.Ctx = ctx
	start := time.Now()
	if res := ev.Decide("account", "read", c); res.Allowed || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("expected fast fail-closed: %+v after %s", res, time.Since(start))
	}
}
