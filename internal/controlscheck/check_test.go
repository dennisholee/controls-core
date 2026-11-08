package controlscheck

import (
	"encoding/json"
	"strings"
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
		"payment.write": {RejectFields: []string{"pan"}},
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

func TestMaskPathTypoIsAnError(t *testing.T) {
	b := fixtureBundle()
	b.Masks.Policies["account.read"] = config.MaskPolicy{
		Fields: map[string]config.MaskField{"ssnTypo": {Mode: "full"}},
	}
	res := Validate(b, fixtureDoc(), nil)
	found := false
	for _, err := range res.Errors {
		if strings.Contains(err, "ssnTypo") {
			found = true
		}
	}
	if !found {
		t.Fatalf("mask path typo was not an error: %v", res.Errors)
	}
}

func TestDQMustCompile(t *testing.T) {
	b := fixtureBundle()
	b.DQ.Rules = append(b.DQ.Rules, config.DQRule{ID: "bad", ResourceType: "payment", Field: "amount", Op: "greaterThan"})
	res := Validate(b, fixtureDoc(), nil)
	found := false
	for _, err := range res.Errors {
		if strings.Contains(err, "unknown op") {
			found = true
		}
	}
	if !found {
		t.Fatalf("invalid DQ operator was not reported: %v", res.Errors)
	}
}

func TestDQActionMustBeRegistered(t *testing.T) {
	b := fixtureBundle()
	b.DQ.Rules[0].Action = "read"
	res := Validate(b, fixtureDoc(), nil)
	found := false
	for _, err := range res.Errors {
		if strings.Contains(err, `action "read" not registered`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("unregistered DQ action was not reported: %v", res.Errors)
	}
}

func endpointFixture() (*config.Bundle, *openapidiff.Document) {
	b := fixtureBundle()
	b.Registry.Endpoints = append(b.Registry.Endpoints, config.Endpoint{
		ID: "mergePayment", Method: "POST", Path: "/payments/{id}/merges",
		ResourceType: "payment", Action: "write",
		ResourceID: config.Ref{From: "path", Name: "id"}, ResourceFrom: "pip",
		MaskPolicy: "payment.merge",
	})
	b.Masks.Policies["payment.merge"] = config.MaskPolicy{}
	doc := fixtureDoc()
	doc.Paths["/payments/{id}/merges"] = map[string]json.RawMessage{
		"post": json.RawMessage(`{
		  "requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Merge"}}}},
		  "responses":{"201":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/PaymentResult"}}}}}
		}`),
	}
	doc.Components.Schemas["Merge"] = json.RawMessage(`{"type":"object","properties":{"sourceId":{"type":"string"}}}`)
	return b, doc
}

func TestDQEndpointScopedField(t *testing.T) {
	b, doc := endpointFixture()
	b.DQ.Rules = []config.DQRule{{
		ID: "merge-source", ResourceType: "payment", Action: "write",
		Endpoint: "mergePayment", Field: "sourceId", Required: true,
	}}
	res := Validate(b, doc, nil)
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
}

func TestDQEndpointScopedFieldMustExist(t *testing.T) {
	b, doc := endpointFixture()
	b.DQ.Rules = []config.DQRule{{
		ID: "merge-source", ResourceType: "payment", Action: "write",
		Endpoint: "mergePayment", Field: "missing", Required: true,
	}}
	res := Validate(b, doc, nil)
	found := false
	for _, err := range res.Errors {
		if strings.Contains(err, "not in request schema for \"mergePayment\"") {
			found = true
		}
	}
	if !found {
		t.Fatalf("endpoint-scoped field error missing: %v", res.Errors)
	}
}

func TestDQUnknownEndpoint(t *testing.T) {
	b, doc := endpointFixture()
	b.DQ.Rules = []config.DQRule{{
		ID: "merge-source", ResourceType: "payment", Action: "write",
		Endpoint: "missing-endpoint", Field: "sourceId", Required: true,
	}}
	res := Validate(b, doc, nil)
	found := false
	for _, err := range res.Errors {
		if strings.Contains(err, "unknown endpoint") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown endpoint error missing: %v", res.Errors)
	}
}
