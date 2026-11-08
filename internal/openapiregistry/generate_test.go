package openapiregistry

import (
	"encoding/json"
	"testing"

	"example.com/controls/core/internal/openapidiff"
)

func TestGenerate(t *testing.T) {
	doc := openapidiff.Document{Paths: map[string]map[string]json.RawMessage{
		"/accounts/{id}": {"get": json.RawMessage(`{
			"operationId":"get-account",
			"x-resource-type":"account",
			"x-resource-id":"id",
			"x-mask-policy":"account.read",
			"x-attributes":["ownerId"]
		}`)},
		"/payments": {"post": json.RawMessage(`{
			"operationId":"createPayment",
			"x-resource-type":"payment",
			"x-resource-id":"$._id",
			"x-mask-policy":"payment.write",
			"x-self-describing":true,
			"x-allow-non-json":true
		}`)},
	}}
	reg, err := Generate(doc, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Endpoints) != 2 {
		t.Fatalf("endpoints = %d", len(reg.Endpoints))
	}
	if reg.Endpoints[0].ID != "create-payment" || reg.Endpoints[0].Action != "write" || reg.Endpoints[0].ResourceID.From != "body" {
		t.Fatalf("payment = %+v", reg.Endpoints[0])
	}
	if reg.Endpoints[0].Attributes != nil {
		t.Fatalf("unexpected attributes: %v", reg.Endpoints[0].Attributes)
	}
	if reg.Endpoints[1].ResourceID.From != "path" || reg.Endpoints[1].Action != "read" {
		t.Fatalf("account = %+v", reg.Endpoints[1])
	}
}

func TestGenerateNormalizesOperationID(t *testing.T) {
	doc := openapidiff.Document{Paths: map[string]map[string]json.RawMessage{
		"/x": {"get": json.RawMessage(`{
			"operationId":"getPartyByID",
			"x-resource-type":"party",
			"x-resource-id":"id",
			"x-mask-policy":"party.read"
		}`)},
	}}
	reg, err := Generate(doc, "party")
	if err != nil {
		t.Fatal(err)
	}
	if got := reg.Endpoints[0].ID; got != "get-party-by-id" {
		t.Fatalf("id = %q", got)
	}
}

func TestGenerateRequiresBindings(t *testing.T) {
	doc := openapidiff.Document{Paths: map[string]map[string]json.RawMessage{
		"/x": {"get": json.RawMessage(`{"x-resource-type":"x"}`)},
	}}
	if _, err := Generate(doc, ""); err == nil {
		t.Fatal("expected missing binding error")
	}
}
