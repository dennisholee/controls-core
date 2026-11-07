package controlscheck

import (
	"encoding/json"
	"testing"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/openapidiff"
)

func fixtureDoc() *openapidiff.Document {
	return &openapidiff.Document{
		Paths: map[string]map[string]json.RawMessage{
			"/accounts/{id}": {"get": json.RawMessage(`{
			  "responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Account"}}}}}
			}`)},
			"/payments": {"post": json.RawMessage(`{
			  "requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Payment"}}}},
			  "responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PaymentResult"}}}}}
			}`)},
		},
		Components: openapidiff.Components{Schemas: map[string]json.RawMessage{
			"Account":       json.RawMessage(`{"type":"object","properties":{"ssn":{"type":"string"},"holder":{"type":"object","properties":{"email":{"type":"string"}}}}}`),
			"Payment":       json.RawMessage(`{"type":"object","properties":{"amount":{"type":"number"},"currency":{"type":"string"},"pan":{"type":"string"},"_id":{"type":"string"}}}`),
			"PaymentResult": json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`),
		}},
	}
}

func fixtureBundle() *config.Bundle {
	b := &config.Bundle{}
	b.Registry.Endpoints = []config.Endpoint{
		{
			ID: "getAccount", Method: "GET", Path: "/accounts/{id}", ResourceType: "account", Action: "read",
			ResourceID: config.Ref{From: "path", Name: "id"}, ResourceFrom: "pip",
			Attributes: []string{"ownerId"}, MaskPolicy: "account.read",
		},
		{
			ID: "createPayment", Method: "POST", Path: "/payments", ResourceType: "payment", Action: "write",
			ResourceID: config.Ref{From: "body", Path: "$._id"}, ResourceFrom: "body",
			MaskPolicy: "payment.write",
		},
	}
	b.Masks.Policies = map[string]config.MaskPolicy{
		"account.read":  {Fields: map[string]config.MaskField{"ssn": {Mode: "full"}, "holder.email": {Mode: "partial"}}},
		"payment.write": {RejectFields: []string{"pan"}, Fields: map[string]config.MaskField{"pan": {Mode: "full"}}},
	}
	b.DQ.Rules = []config.DQRule{{ID: "amount", ResourceType: "payment", Field: "amount", Required: true}}
	b.Rules.Rules = []config.Rule{{ID: "pay", Effect: "allow", ResourceType: "payment", Action: "write"}}
	return b
}

func TestValid(t *testing.T) {
	res := Validate(fixtureBundle(), fixtureDoc(), map[string]any{
		"resources": map[string]any{"account": map[string]any{"A1": map[string]any{"ownerId": "u-1"}}},
	})
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
}

func TestUnknownReferences(t *testing.T) {
	b := fixtureBundle()
	b.Rules.Rules[0].ResourceType = "refund"
	b.DQ.Rules[0].Field = "amountDue"
	b.Masks.Policies["account.read"] = config.MaskPolicy{Fields: map[string]config.MaskField{"missing": {Mode: "full"}}}
	res := Validate(b, fixtureDoc(), nil)
	if len(res.Errors) < 2 {
		t.Fatalf("expected at least 2 errors, got %v", res.Errors)
	}
}
