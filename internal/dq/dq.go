// Package dq evaluates request data-quality rules.
//
// DQ is multi-state by design: a violation is reported even when it does not
// block the request. Only rules marked blocking cause a rejection; everything
// else feeds decision logs, metrics and (in production) the quarantine path.
package dq

import (
	"encoding/json"
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

type rule struct {
	cfg config.DQRule
	re  *regexp.Regexp
}

// Evaluator runs DQ rules against a request body.
type Evaluator struct {
	rules []rule
}

// New compiles DQ rules, rejecting unknown operators/types and invalid
// patterns at load time rather than turning them into runtime violations.
func New(b *config.Bundle) (*Evaluator, error) {
	e := &Evaluator{}
	for _, r := range b.DQ.Rules {
		compiled := rule{cfg: r}
		switch r.Op {
		case "", "pattern", "maxLength", "type", "eq", "neq", "gt", "gte", "lt", "lte":
		default:
			return nil, fmt.Errorf("dq rule %q: unknown op %q", r.ID, r.Op)
		}
		if r.Op == "pattern" {
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("dq rule %q: invalid pattern: %w", r.ID, err)
			}
			compiled.re = re
		}
		if r.Op == "type" {
			want, _ := r.Value.(string)
			if want != "string" && want != "number" {
				return nil, fmt.Errorf("dq rule %q: type must be string or number", r.ID)
			}
		}
		switch r.Action {
		case "", "*", "read", "write", "delete":
		default:
			return nil, fmt.Errorf("dq rule %q: action must be read, write, delete or *", r.ID)
		}
		e.rules = append(e.rules, compiled)
	}
	return e, nil
}

// Check returns all violations for the given resource type, action and
// document. A rule with no action (or *) applies to every action.
func (e *Evaluator) Check(resourceType, action, endpoint string, doc map[string]any) []Violation {
	var out []Violation
	for _, r := range e.rules {
		if r.cfg.ResourceType != "*" && r.cfg.ResourceType != resourceType {
			continue
		}
		if r.cfg.Action != "" && r.cfg.Action != "*" && r.cfg.Action != action {
			continue
		}
		if r.cfg.Endpoint != "" && r.cfg.Endpoint != endpoint {
			continue
		}
		values, present := lookupValues(doc, r.cfg.Field)
		if r.cfg.Required {
			missing := !present || len(values) == 0
			for _, v := range values {
				if v == nil || v == "" {
					missing = true
				}
			}
			if missing {
				out = append(out, Violation{RuleID: r.cfg.ID, Field: r.cfg.Field, Message: "required field missing", Blocking: r.cfg.Blocking})
				continue
			}
		}
		if !present {
			continue
		}
		for _, value := range values {
			if msg := check(r, value); msg != "" {
				out = append(out, Violation{RuleID: r.cfg.ID, Field: r.cfg.Field, Message: msg, Blocking: r.cfg.Blocking})
				break
			}
		}
	}
	return out
}

func check(r rule, value any) string {
	switch r.cfg.Op {
	case "":
		return ""
	case "pattern":
		if !r.re.MatchString(fmt.Sprint(value)) {
			return "pattern mismatch"
		}
	case "maxLength":
		if len(fmt.Sprint(value)) > r.cfg.MaxLength {
			return fmt.Sprintf("exceeds max length %d", r.cfg.MaxLength)
		}
	case "type":
		want, _ := r.cfg.Value.(string)
		if !hasType(value, want) {
			return "type mismatch, want " + want
		}
	case "eq", "neq", "gt", "gte", "lt", "lte":
		if !compare(value, r.cfg.Op, r.cfg.Value) {
			return "value violates " + r.cfg.Op
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
		case float64, float32, int, int64, json.Number:
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
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// lookupValues resolves a dot path, expanding [*] array wildcards. A scalar
// path returns one value; a wildcard path returns one value per element.
func lookupValues(doc map[string]any, path string) ([]any, bool) {
	current := []any{doc}
	for _, part := range strings.Split(path, ".") {
		wildcard := strings.HasSuffix(part, "[*]")
		key := strings.TrimSuffix(part, "[*]")
		next := []any{}
		for _, node := range current {
			m, ok := node.(map[string]any)
			if !ok {
				continue
			}
			value, ok := m[key]
			if !ok {
				continue
			}
			if wildcard {
				items, ok := value.([]any)
				if !ok {
					continue
				}
				next = append(next, items...)
				continue
			}
			next = append(next, value)
		}
		if len(next) == 0 {
			return nil, false
		}
		current = next
	}
	return current, true
}
