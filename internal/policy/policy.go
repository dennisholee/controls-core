// Package policy evaluates authorization rules.
//
// The Evaluator interface lets a bundle select the reference rules, Cedar, or
// an external OPA decision service without changing the sidecar.
// The reference engine is intentionally small and total: no I/O, no reflection,
// deterministic, and safe to run on the request path.
package policy

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"example.com/controls/core/internal/config"
)

// Context is the input to a decision.
type Context struct {
	Subject    map[string]any
	Resource   map[string]any
	ResourceID string
	Request    map[string]any
}

// Result is a decision plus the rule that produced it.
type Result struct {
	Allowed bool
	Reason  string
}

// Evaluator decides whether a subject may perform an action on a resource.
type Evaluator interface {
	Decide(resourceType, action string, ctx Context) Result
}

// RuleEvaluator is the reference structured-rule implementation.
type RuleEvaluator struct {
	rules         []config.Rule
	defaultEffect string
}

// New builds the reference rule evaluator from a validated bundle.
func New(b *config.Bundle) *RuleEvaluator {
	return &RuleEvaluator{rules: b.Rules.Rules, defaultEffect: b.Rules.DefaultEffect}
}

// Options configures evaluators that need external endpoints.
type Options struct {
	OPAURL     string
	OPATimeout time.Duration
}

// NewEvaluator selects the evaluator named by the bundle with default options.
func NewEvaluator(b *config.Bundle) (Evaluator, error) {
	return NewEvaluatorWithOptions(b, Options{})
}

// NewEvaluatorWithOptions selects the evaluator named by the bundle.
func NewEvaluatorWithOptions(b *config.Bundle, opts Options) (Evaluator, error) {
	switch b.Rules.Engine {
	case "", "rules":
		return New(b), nil
	case "cedar":
		return NewCedarEvaluator(b.CedarPolicies)
	case "opa":
		decision := "controls/authz/allow"
		if b.Rules.OPA != nil && b.Rules.OPA.Decision != "" {
			decision = b.Rules.OPA.Decision
		}
		return NewOPAEvaluator(opts.OPAURL, decision, opts.OPATimeout)
	default:
		return nil, fmt.Errorf("unknown policy engine %q", b.Rules.Engine)
	}
}

// DenyAll is used when the configured evaluator failed to initialise.
type DenyAll struct{}

// Decide always denies.
func (DenyAll) Decide(string, string, Context) Result {
	return Result{Allowed: false, Reason: "policy_init_error"}
}

// Decide applies deny rules first (absolute), then allow rules (OR-ed).
func (e *RuleEvaluator) Decide(resourceType, action string, ctx Context) Result {
	for _, r := range e.rules {
		if r.Effect != "deny" || !applies(r, resourceType, action) {
			continue
		}
		if evalExpr(r.When, ctx) {
			return Result{Allowed: false, Reason: "deny:" + r.ID}
		}
	}
	allowRule := ""
	for _, r := range e.rules {
		if r.Effect != "allow" || !applies(r, resourceType, action) {
			continue
		}
		allowRule = r.ID
		if evalExpr(r.When, ctx) {
			return Result{Allowed: true, Reason: "allow:" + r.ID}
		}
	}
	if e.defaultEffect == "allow" {
		return Result{Allowed: true, Reason: "default:allow"}
	}
	if allowRule != "" {
		return Result{Allowed: false, Reason: "default:deny(no-allow-match)"}
	}
	return Result{Allowed: false, Reason: "default:deny"}
}

func applies(r config.Rule, resourceType, action string) bool {
	if r.ResourceType != "*" && r.ResourceType != resourceType {
		return false
	}
	if r.Action != "*" && r.Action != action {
		return false
	}
	return true
}

func evalExpr(e config.Expr, ctx Context) bool {
	switch {
	case len(e.All) > 0:
		for _, sub := range e.All {
			if !evalExpr(sub, ctx) {
				return false
			}
		}
		return true
	case len(e.Any) > 0:
		for _, sub := range e.Any {
			if evalExpr(sub, ctx) {
				return true
			}
		}
		return false
	}
	if e.Field == "" {
		return false
	}
	actual, ok := resolve(e.Field, ctx)
	if !ok {
		return false
	}
	want := e.Value
	if s, isStr := want.(string); isStr && strings.HasPrefix(s, "$") {
		if ref, ok := resolve(strings.TrimPrefix(s, "$"), ctx); ok {
			want = ref
		} else {
			return false
		}
	}
	return compare(actual, e.Op, want)
}

func resolve(path string, ctx Context) (any, bool) {
	parts := strings.Split(path, ".")
	var cur any
	switch parts[0] {
	case "subject":
		cur = ctx.Subject
	case "resource":
		cur = ctx.Resource
	case "request":
		cur = ctx.Request
	default:
		return nil, false
	}
	for _, p := range parts[1:] {
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

func compare(actual any, op string, want any) bool {
	switch op {
	case "exists":
		return actual != nil
	case "eq", "":
		return equal(actual, want)
	case "neq":
		return !equal(actual, want)
	case "in":
		list, ok := want.([]any)
		if !ok {
			return false
		}
		for _, item := range list {
			if equal(actual, item) {
				return true
			}
		}
		return false
	case "contains":
		s, ok1 := actual.(string)
		w, ok2 := want.(string)
		return ok1 && ok2 && strings.Contains(s, w)
	case "gt", "gte", "lt", "lte":
		a, ok1 := toFloat(actual)
		b, ok2 := toFloat(want)
		if !ok1 || !ok2 {
			return false
		}
		switch op {
		case "gt":
			return a > b
		case "gte":
			return a >= b
		case "lt":
			return a < b
		default:
			return a <= b
		}
	}
	return false
}

func equal(a, b any) bool {
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
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
		f, err := strconv.ParseFloat(string(n), 64)
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}
