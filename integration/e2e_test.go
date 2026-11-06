package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/pipclient"
	"example.com/controls/core/pip/server"
)

func newEngine(t *testing.T) (*engine.Engine, *httptest.Server) {
	t.Helper()
	repo, err := server.LoadMemoryRepo("../pip/fixtures/attributes.json")
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pip := httptest.NewServer(server.New(repo, log).Handler())
	t.Cleanup(pip.Close)

	bundle, err := config.LoadDir("testdata/config")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	eng := engine.New(bundle, pipclient.New(pip.URL))
	return eng, pip
}

func subject(sub string, claims map[string]string) engine.Subject {
	return engine.Subject{Sub: sub, Claims: claims}
}

func TestAccountOwnerAllowedAndMasked(t *testing.T) {
	eng, _ := newEngine(t)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "GET", Path: "/accounts/A123",
		Subject: subject("u-1", map[string]string{"region": "US"}),
	})
	if !res.Allowed {
		t.Fatalf("expected allow, got %+v", res)
	}
	body := []byte(`{"accountNumber":"1234567890","holder":{"email":"jane.doe@example.com"},"region":"US","ssn":"111-22-3333"}`)
	masked, err := eng.MaskResponse(eng.Endpoint("GET", "/accounts/A123"), body, false)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(masked, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["accountNumber"] != "******7890" {
		t.Errorf("accountNumber = %v", doc["accountNumber"])
	}
	if doc["ssn"] != "[REDACTED]" {
		t.Errorf("ssn = %v", doc["ssn"])
	}
	holder := doc["holder"].(map[string]any)
	if holder["email"] != "j***@example.com" {
		t.Errorf("email = %v", holder["email"])
	}
	if doc["region"] != "US" {
		t.Errorf("region should be untouched, got %v", doc["region"])
	}
}

func TestAccountOtherRegionDenied(t *testing.T) {
	eng, _ := newEngine(t)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "GET", Path: "/accounts/A123",
		Subject: subject("u-2", map[string]string{"region": "EU"}),
	})
	if res.Allowed || res.Status != 403 {
		t.Fatalf("expected deny 403, got %+v", res)
	}
}

func TestAccountRegionClaimAllowed(t *testing.T) {
	eng, _ := newEngine(t)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "GET", Path: "/accounts/A123",
		Subject: subject("u-9", map[string]string{"region": "US"}),
	})
	if !res.Allowed {
		t.Fatalf("expected allow via region claim, got %+v", res)
	}
}

func TestDenyRuleWins(t *testing.T) {
	eng, _ := newEngine(t)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "GET", Path: "/accounts/A123",
		Subject: subject("u-1", map[string]string{"status": "sanctioned"}),
	})
	if res.Allowed {
		t.Fatalf("expected deny, got %+v", res)
	}
}

func TestSelfDescribingTypeMismatch(t *testing.T) {
	eng, _ := newEngine(t)
	body := []byte(`{"_type":"account","_id":"P-9001","amount":10,"ownerId":"u-1"}`)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "POST", Path: "/payments", Body: body, Subject: subject("u-1", nil),
	})
	if res.Allowed || res.Reason != "self_describing_type_mismatch" {
		t.Fatalf("expected type mismatch deny, got %+v", res)
	}
}

func TestPaymentAllowedAndDQ(t *testing.T) {
	eng, _ := newEngine(t)
	body := []byte(`{"_type":"payment","_id":"P-9001","amount":10,"ownerId":"u-1"}`)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "POST", Path: "/payments", Body: body, Subject: subject("u-1", nil),
	})
	if !res.Allowed {
		t.Fatalf("expected allow, got %+v", res)
	}
}

func TestPaymentBlockingDQ(t *testing.T) {
	eng, _ := newEngine(t)
	body := []byte(`{"_type":"payment","_id":"P-9001","amount":-5,"ownerId":"u-1"}`)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "POST", Path: "/payments", Body: body, Subject: subject("u-1", nil),
	})
	if res.Allowed || res.Status != 403 {
		t.Fatalf("expected blocking DQ deny, got %+v", res)
	}
}

func TestRejectedField(t *testing.T) {
	eng, _ := newEngine(t)
	body := []byte(`{"_type":"payment","_id":"P-9001","amount":10,"ownerId":"u-1","pan":"4111111111111111"}`)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "POST", Path: "/payments", Body: body, Subject: subject("u-1", nil),
	})
	if res.Allowed || res.Reason != "rejected_field:pan" {
		t.Fatalf("expected rejected field deny, got %+v", res)
	}
}

func TestUnregisteredEndpointDenied(t *testing.T) {
	eng, _ := newEngine(t)
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "GET", Path: "/admin/secrets", Subject: subject("u-1", nil),
	})
	if res.Allowed || res.Reason != "unregistered_endpoint" {
		t.Fatalf("expected unregistered deny, got %+v", res)
	}
}

func TestPIPUnavailableFailsClosed(t *testing.T) {
	_, pip := newEngine(t)
	pip.Close() // simulate PIP outage

	bundle, err := config.LoadDir("testdata/config")
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.New(bundle, pipclient.New(pip.URL))
	res := eng.ProcessRequest(context.Background(), engine.Request{
		Method: "GET", Path: "/accounts/A123", Subject: subject("u-1", nil),
	})
	if res.Allowed || res.Status != 503 {
		t.Fatalf("expected fail-closed 503, got %+v", res)
	}
}

func TestMaskErrors(t *testing.T) {
	eng, _ := newEngine(t)
	body := []byte(`{"error":"boom","ssn":"111-22-3333"}`)
	masked, err := eng.MaskResponse(eng.Endpoint("GET", "/accounts/A123"), body, true)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(masked, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["ssn"] != "[REDACTED]" {
		t.Errorf("error body ssn not masked: %v", doc["ssn"])
	}
}
