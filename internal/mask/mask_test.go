package mask

import (
	"testing"

	"example.com/controls/core/internal/config"
)

func TestPartialSuffix(t *testing.T) {
	doc := map[string]any{"accountNumber": "1234567890"}
	Apply(config.MaskPolicy{Fields: map[string]config.MaskField{
		"accountNumber": {Mode: "partial", KeepSuffix: 4},
	}}, doc)
	if got := doc["accountNumber"]; got != "******7890" {
		t.Fatalf("got %v", got)
	}
}

func TestPartialEmailPreservesDomain(t *testing.T) {
	doc := map[string]any{"holder": map[string]any{"email": "jane.doe@example.com"}}
	Apply(config.MaskPolicy{Fields: map[string]config.MaskField{
		"holder.email": {Mode: "partial", KeepPrefix: 1, PreserveDomain: true},
	}}, doc)
	holder := doc["holder"].(map[string]any)
	if holder["email"] != "j***@example.com" {
		t.Fatalf("got %v", holder["email"])
	}
}

func TestPartialYear(t *testing.T) {
	doc := map[string]any{"dob": "1990-01-02"}
	Apply(config.MaskPolicy{Fields: map[string]config.MaskField{
		"dob": {Mode: "partial", Keep: "year"},
	}}, doc)
	if doc["dob"] != "1990******" {
		t.Fatalf("got %v", doc["dob"])
	}
}

func TestArrayWildcard(t *testing.T) {
	doc := map[string]any{"items": []any{
		map[string]any{"price": "12345"},
		map[string]any{"price": "98765"},
	}}
	Apply(config.MaskPolicy{Fields: map[string]config.MaskField{
		"items[*].price": {Mode: "partial", KeepSuffix: 2},
	}}, doc)
	items := doc["items"].([]any)
	if items[0].(map[string]any)["price"] != "***45" {
		t.Fatalf("got %v", items[0])
	}
	if items[1].(map[string]any)["price"] != "***65" {
		t.Fatalf("got %v", items[1])
	}
}

func TestDefaultFull(t *testing.T) {
	doc := map[string]any{"a": "x", "b": map[string]any{"c": "y"}, "n": 42}
	Apply(config.MaskPolicy{Default: "full", Fields: map[string]config.MaskField{
		"a": {Mode: "none"},
	}}, doc)
	if doc["a"] != "x" {
		t.Fatalf("explicit none overridden: %v", doc["a"])
	}
	if doc["b"].(map[string]any)["c"] != "[REDACTED]" {
		t.Fatalf("nested not masked: %v", doc["b"])
	}
	if doc["n"] != nil {
		t.Fatalf("non-string should be null: %v", doc["n"])
	}
}

func TestFullString(t *testing.T) {
	doc := map[string]any{"ssn": "111-22-3333"}
	Apply(config.MaskPolicy{Fields: map[string]config.MaskField{
		"ssn": {Mode: "full"},
	}}, doc)
	if doc["ssn"] != "[REDACTED]" {
		t.Fatalf("got %v", doc["ssn"])
	}
}
