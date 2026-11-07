package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/pipclient"
)

type classPIP struct {
	classes map[string]string
	err     error
}

func (c classPIP) Resolve(context.Context, []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	return map[string]pipclient.Attribute{}, nil
}
func (c classPIP) Classification(context.Context, string) (map[string]string, error) {
	return c.classes, c.err
}
func (c classPIP) CachedClassification(string) (map[string]string, bool) { return nil, false }

func fallbackBundle() *config.Bundle {
	b := &config.Bundle{}
	b.Registry.Endpoints = []config.Endpoint{{
		ID: "e", Method: "POST", Path: "/x", ResourceType: "account",
		ResourceID: config.Ref{From: "body", Path: "$._id"}, ResourceFrom: "body",
		MaskPolicy: "p",
	}}
	b.Masks.Policies = map[string]config.MaskPolicy{
		"p": {
			Fallback: "classification",
			Fields: map[string]config.MaskField{
				"accountNumber": {Mode: "partial", KeepSuffix: 4},
			},
		},
	}
	return b
}

func TestMaskFallbackUsesClassification(t *testing.T) {
	e := New(fallbackBundle(), classPIP{classes: map[string]string{"phone": "pii.locked"}})
	ep := e.Endpoint("POST", "/x")
	out, err := e.MaskResponseCtx(context.Background(), ep,
		[]byte(`{"accountNumber":"1234567890","phone":"+15551234567","status":"active"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["phone"] != "[REDACTED]" {
		t.Fatalf("phone not masked: %v", doc)
	}
	if doc["accountNumber"] != "******7890" {
		t.Fatalf("explicit rule lost: %v", doc)
	}
	if doc["status"] != "active" {
		t.Fatalf("unclassified changed: %v", doc)
	}
}

func TestMaskFallbackFailsClosedWithoutClassification(t *testing.T) {
	e := New(fallbackBundle(), classPIP{err: errors.New("pip down")})
	ep := e.Endpoint("POST", "/x")
	if _, err := e.MaskResponseCtx(context.Background(), ep, []byte(`{"phone":"x"}`), false); err == nil {
		t.Fatal("expected classification error")
	}
}
