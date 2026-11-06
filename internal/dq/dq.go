// Package dq evaluates request data-quality rules.
//
// DQ is multi-state by design: a violation is reported even when it does not
// block the request. Only rules marked blocking cause a rejection; everything
// else feeds decision logs, metrics and (in production) the quarantine path.
package dq

import (
	"fmt"
	"regexp"
	"strings"

	"example.com/controls/core/internal/config"
)

// Violation is one failed check.
type Violation struct {
	RuleID   string `json:"ruleId"`
	Field    string `json:"field"`
	Message  string `json:"message"`
	Blocking bool   `json:"blocking"`
}

// Evaluator runs DQ rules against a request body.
type Evaluator struct {
	rules []config.DQRule
}

// New builds an evaluator from a validated bundle.
func New(b *config.Bundle) *Evaluator {
	return &Evaluator{rules: b.DQ.Rules}
}

// Check returns all violations for the given resource type and document.
func (e *Evaluator) Check(resourceType string, doc map[string]any) []Violation {
	var out []Violation
	for _, r := range e.rules {
		if r.ResourceType != "*" && r.ResourceType != resourceType {
			continue
		}
		value, present := lookup(doc, r.Field)
		if r.Required && (!present || value == nil || value == "") {
			out = append(out, Violation{RuleID: r.ID, Field: r.Field, Message: "required field missing", Blocking: r.Blocking})
			continue
		}
		if !present {
			continue
		}
		if msg := check(r, value); msg != "" {
			out = append(out, Violation{RuleID: r.ID, Field: r.Field, Message: msg, Blocking: r.Blocking})
		}
	}
	return out
}

func check(r config.DQRule, value any) string {
	switch r.Op {
	case "":
		return ""
	case "pattern":
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return "invalid rule pattern"
		}
		if !re.MatchString(fmt.Sprint(value)) {
			return "pattern mismatch"
		}
	case "maxLength":
		if len(fmt.Sprint(value)) > r.MaxLength {
			return fmt.Sprintf("exceeds max length %d", r.MaxLength)
		}
	case "type":
		want, _ := r.Value.(string)
		if !hasType(value, want) {
			return "type mismatch, want " + want
		}
	case "eq", "neq", "gt", "gte", "lt", "lte":
		if !compare(value, r.Op, r.Value) {
			return "value violates " + r.Op
		}
	}
	return ""
}

func hasType(value any, want string) bool {
	switch want {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case float64, float32, int, int64:
			return true
		}
		return false
	}
	return true
}

func compare(a any, op string, b any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		switch op {
		case "eq":
			return af == bf
		case "neq":
			return af != bf
		case "gt":
			return af > bf
		case "gte":
			return af >= bf
		case "lt":
			return af < bf
		case "lte":
			return af <= bf
		}
	}
	if op == "eq" {
		return fmt.Sprint(a) == fmt.Sprint(b)
	}
	if op == "neq" {
		return fmt.Sprint(a) != fmt.Sprint(b)
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func lookup(doc map[string]any, path string) (any, bool) {
	var cur any = doc
	for _, p := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
