// Package config loads and validates the controls bundle: endpoint registry,
// authorization rules, masking policies and data-quality rules.
//
// The bundle is produced by the delivery pipeline (controls-rules repo),
// validated against contracts/ in CI, and injected into the sidecar at deploy
// time. Formats are JSON here to keep the reference implementation dependency
// free; the published schemas are format-agnostic and YAML is supported by the
// delivery pipeline's renderer.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Registry is the endpoint registry. Resource type and identity are bound to
// the route here; payload self-description is only ever a cross-check.
type Registry struct {
	Version             string     `json:"version"`
	DefaultUnregistered string     `json:"defaultUnregistered"` // deny | allow
	Endpoints           []Endpoint `json:"endpoints"`
}

// Endpoint is one configured route.
type Endpoint struct {
	ID           string   `json:"id"`
	Service      string   `json:"service,omitempty"`
	Method       string   `json:"method"`
	Path         string   `json:"path"`
	ResourceType string   `json:"resourceType"`
	Action       string   `json:"action"`
	ResourceID   Ref      `json:"resourceId"`
	ResourceFrom string   `json:"resourceSource"` // pip | body | both
	Attributes   []string `json:"attributes"`
	MaskPolicy   string   `json:"maskPolicy"`
	MaskErrors   bool     `json:"maskErrors"`
	Freshness    string   `json:"freshness"` // cached | fresh | deny-on-stale
	// MaxStalenessSeconds bounds replica lag for fresh/deny-on-stale
	// endpoints. Zero means the default (5s).
	MaxStalenessSeconds int  `json:"maxStalenessSeconds,omitempty"`
	SelfDescribe        bool `json:"selfDescribing"`
}

// Ref locates the resource identifier in the path or the request body.
type Ref struct {
	From string `json:"from"` // path | body
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// Rules is the authorization rule set. Deny rules are evaluated first and are
// absolute; allow rules are then OR-ed. No match falls back to DefaultEffect.
type Rules struct {
	Version       string `json:"version"`
	DefaultEffect string `json:"defaultEffect"` // allow | deny
	Rules         []Rule `json:"rules"`
}

// Rule is a single authorization rule.
type Rule struct {
	ID           string `json:"id"`
	Effect       string `json:"effect"` // allow | deny
	ResourceType string `json:"resourceType"`
	Action       string `json:"action"`
	When         Expr   `json:"when"`
}

// Expr is a small structured predicate. It is deliberately not a general
// language: the production policy engine may be swapped for OPA or Cedar
// behind the policy.Evaluator interface.
type Expr struct {
	All   []Expr `json:"all,omitempty"`
	Any   []Expr `json:"any,omitempty"`
	Field string `json:"field,omitempty"`
	Op    string `json:"op,omitempty"`
	Value any    `json:"value,omitempty"`
}

// MaskPolicies maps a policy name to field masking configuration.
type MaskPolicies struct {
	Version  string                `json:"version"`
	Policies map[string]MaskPolicy `json:"policies"`
}

// MaskPolicy describes how a response is masked for a journey.
type MaskPolicy struct {
	Default      string               `json:"default"` // none | full
	Fields       map[string]MaskField `json:"fields"`
	RejectFields []string             `json:"rejectFields,omitempty"`
}

// MaskField configures one field path.
type MaskField struct {
	Mode           string `json:"mode"` // none | full | partial
	KeepPrefix     int    `json:"keepPrefix,omitempty"`
	KeepSuffix     int    `json:"keepSuffix,omitempty"`
	PreserveDomain bool   `json:"preserveDomain,omitempty"`
	Keep           string `json:"keep,omitempty"` // year
}

// DQConfig is the request data-quality rule set.
type DQConfig struct {
	Version string   `json:"version"`
	Rules   []DQRule `json:"rules"`
}

// DQRule is one data-quality check.
type DQRule struct {
	ID           string `json:"id"`
	ResourceType string `json:"resourceType"`
	Field        string `json:"field"`
	Required     bool   `json:"required,omitempty"`
	Op           string `json:"op,omitempty"` // gt|gte|lt|lte|eq|pattern|maxLength|type
	Value        any    `json:"value,omitempty"`
	Pattern      string `json:"pattern,omitempty"`
	MaxLength    int    `json:"maxLength,omitempty"`
	Blocking     bool   `json:"blocking,omitempty"`
}

// Bundle is the complete, validated configuration injected into the sidecar.
type Bundle struct {
	Registry Registry
	Rules    Rules
	Masks    MaskPolicies
	DQ       DQConfig
}

// LockedFields may only be fully redacted. They cannot be relaxed per journey.
var LockedFields = map[string]bool{
	"ssn":        true,
	"pan":        true,
	"cvv":        true,
	"cardNumber": true,
}

// LoadDir reads registry.json, rules.json, mask-policies.json and dq.json.
func LoadDir(dir string) (*Bundle, error) {
	b := &Bundle{}
	if err := readJSON(filepath.Join(dir, "registry.json"), &b.Registry); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "rules.json"), &b.Rules); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "mask-policies.json"), &b.Masks); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "dq.json"), &b.DQ); err != nil {
		return nil, err
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// Validate enforces the invariants the contracts promise. The sidecar refuses
// to start on an invalid bundle rather than run with partial configuration.
func (b *Bundle) Validate() error {
	if b.Registry.DefaultUnregistered != "" &&
		b.Registry.DefaultUnregistered != "deny" && b.Registry.DefaultUnregistered != "allow" {
		return fmt.Errorf("registry: defaultUnregistered must be deny or allow")
	}
	seen := map[string]bool{}
	for i, ep := range b.Registry.Endpoints {
		if ep.ID == "" || ep.Method == "" || ep.Path == "" || ep.ResourceType == "" {
			return fmt.Errorf("registry: endpoint %d missing id/method/path/resourceType", i)
		}
		if seen[ep.ID] {
			return fmt.Errorf("registry: duplicate endpoint id %q", ep.ID)
		}
		seen[ep.ID] = true
		if ep.ResourceID.From != "path" && ep.ResourceID.From != "body" {
			return fmt.Errorf("registry: endpoint %q resourceId.from must be path or body", ep.ID)
		}
		if ep.ResourceID.From == "path" && ep.ResourceID.Name == "" {
			return fmt.Errorf("registry: endpoint %q path resourceId requires name", ep.ID)
		}
		if ep.ResourceID.From == "body" && ep.ResourceID.Path == "" {
			return fmt.Errorf("registry: endpoint %q body resourceId requires path", ep.ID)
		}
		switch ep.ResourceFrom {
		case "", "pip", "body", "both":
		default:
			return fmt.Errorf("registry: endpoint %q resourceSource must be pip, body or both", ep.ID)
		}
		switch ep.Freshness {
		case "", "cached", "fresh", "deny-on-stale":
		default:
			return fmt.Errorf("registry: endpoint %q freshness must be cached, fresh or deny-on-stale", ep.ID)
		}
		if ep.MaxStalenessSeconds < 0 {
			return fmt.Errorf("registry: endpoint %q maxStalenessSeconds must be >= 0", ep.ID)
		}
		if ep.MaskPolicy != "" {
			if _, ok := b.Masks.Policies[ep.MaskPolicy]; !ok {
				return fmt.Errorf("registry: endpoint %q references unknown mask policy %q", ep.ID, ep.MaskPolicy)
			}
		}
	}
	if b.Rules.DefaultEffect != "allow" && b.Rules.DefaultEffect != "deny" {
		return fmt.Errorf("rules: defaultEffect must be allow or deny")
	}
	for _, r := range b.Rules.Rules {
		if r.ID == "" {
			return fmt.Errorf("rules: rule missing id")
		}
		if r.Effect != "allow" && r.Effect != "deny" {
			return fmt.Errorf("rules: rule %q effect must be allow or deny", r.ID)
		}
	}
	for name, p := range b.Masks.Policies {
		if p.Default != "" && p.Default != "none" && p.Default != "full" {
			return fmt.Errorf("masks: policy %q default must be none or full", name)
		}
		for path, f := range p.Fields {
			if f.Mode != "none" && f.Mode != "full" && f.Mode != "partial" {
				return fmt.Errorf("masks: policy %q field %q mode must be none, full or partial", name, path)
			}
			if LockedFields[lastSegment(path)] && f.Mode != "full" {
				return fmt.Errorf("masks: policy %q field %q is locked and must be full", name, path)
			}
		}
	}
	for _, r := range b.DQ.Rules {
		if r.ID == "" || r.Field == "" {
			return fmt.Errorf("dq: rule missing id or field")
		}
	}
	return nil
}

// EndpointFor returns the endpoint matching method and path, plus captured
// path parameters. Matching is exact segment-wise; "{name}" captures one
// segment. No wildcard or regex matching is permitted by the contract.
func (b *Bundle) EndpointFor(method, path string) (*Endpoint, map[string]string) {
	for i := range b.Registry.Endpoints {
		ep := &b.Registry.Endpoints[i]
		if !strings.EqualFold(ep.Method, method) {
			continue
		}
		if params, ok := matchPath(ep.Path, path); ok {
			return ep, params
		}
	}
	return nil, nil
}

func matchPath(pattern, actual string) (map[string]string, bool) {
	p := splitPath(pattern)
	a := splitPath(actual)
	if len(p) != len(a) {
		return nil, false
	}
	params := map[string]string{}
	for i := range p {
		if strings.HasPrefix(p[i], "{") && strings.HasSuffix(p[i], "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(p[i], "{"), "}")
			if a[i] == "" {
				return nil, false
			}
			params[name] = a[i]
			continue
		}
		if p[i] != a[i] {
			return nil, false
		}
	}
	return params, true
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func lastSegment(path string) string {
	parts := strings.Split(path, ".")
	return parts[len(parts)-1]
}

// SortedPolicies returns mask policy names in deterministic order (useful for
// generated documentation and startup logs).
func (b *Bundle) SortedPolicies() []string {
	names := make([]string, 0, len(b.Masks.Policies))
	for n := range b.Masks.Policies {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
