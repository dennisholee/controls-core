// Package openapidiff compares a service's OpenAPI document with its endpoint
// registry. It is the gate that prevents an endpoint from shipping without a
// registry entry (uncontrolled) or a registry entry from going stale.
package openapidiff

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Document is the subset of an OpenAPI 3 document the gate needs.
type Document struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components Components                            `json:"components"`
}

// Components is the OpenAPI components object.
type Components struct {
	Schemas map[string]json.RawMessage `json:"schemas"`
}

// Endpoint is a registry entry.
type Endpoint struct {
	ID                  string
	Method              string
	Path                string
	ResourceType        string
	ResourceIDFrom      string // path | body
	ResourceID          string // parameter name or $.-path
	MaskPolicy          string
	ResourceSource      string
	Action              string
	Attributes          []string
	Freshness           string
	MaskErrors          *bool
	SelfDescribing      bool
	MaxStalenessSeconds int
	AllowNonJSON        *bool
}

var operations = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// Result is the set of differences.
type Result struct {
	MissingRegistry []string // documented operations with no registry entry
	OrphanRegistry  []string // registry entries with no documented operation
	Mismatches      []string // annotation and registry disagree
}

type operation struct {
	ResourceType        string   `json:"x-resource-type"`
	ResourceID          string   `json:"x-resource-id"`
	MaskPolicy          string   `json:"x-mask-policy"`
	ResourceSource      string   `json:"x-resource-source"`
	Action              string   `json:"x-action"`
	Attributes          []string `json:"x-attributes"`
	Freshness           string   `json:"x-freshness"`
	MaskErrors          *bool    `json:"x-mask-errors"`
	SelfDescribing      *bool    `json:"x-self-describing"`
	MaxStalenessSeconds int      `json:"x-max-staleness-seconds"`
	AllowNonJSON        *bool    `json:"x-allow-non-json"`
	Ignore              bool     `json:"x-controls-ignore"`
}

// Load reads an OpenAPI JSON or YAML document.
func Load(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		var v any
		if err := yaml.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		raw, err = json.Marshal(v)
		if err != nil {
			return nil, err
		}
	}
	var doc Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &doc, nil
}

// Check compares documented operations against registry entries.
func Check(doc Document, endpoints []Endpoint) Result {
	documented := map[string]bool{}

	var res Result
	ops := map[string]operation{}
	for path, item := range doc.Paths {
		for method, raw := range item {
			m := strings.ToLower(method)
			if !operations[m] {
				continue
			}
			var op operation
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &op)
			}
			// x-controls-ignore routes are intentionally absent from the
			// registry and must not be reported as missing coverage.
			if op.Ignore {
				continue
			}
			key := opKey(m, path)
			documented[key] = true
			ops[key] = op
		}
	}
	registeredByKey := registryByKey(endpoints)
	for key := range documented {
		if _, ok := registeredByKey[key]; !ok {
			res.MissingRegistry = append(res.MissingRegistry, key)
			continue
		}
		res.Mismatches = append(res.Mismatches, compareAnnotations(key, ops[key], registeredByKey[key])...)
	}
	for key, ep := range registeredByKey {
		if !documented[key] {
			res.OrphanRegistry = append(res.OrphanRegistry, key+" ("+ep.ID+")")
		}
	}
	sort.Strings(res.MissingRegistry)
	sort.Strings(res.OrphanRegistry)
	return res
}

func compareAnnotations(key string, op operation, ep Endpoint) []string {
	var out []string
	if op.ResourceType != "" && op.ResourceType != ep.ResourceType {
		out = append(out, fmt.Sprintf("%s: x-resource-type %q != registry %q", key, op.ResourceType, ep.ResourceType))
	}
	if op.MaskPolicy != "" && op.MaskPolicy != ep.MaskPolicy {
		out = append(out, fmt.Sprintf("%s: x-mask-policy %q != registry %q", key, op.MaskPolicy, ep.MaskPolicy))
	}
	if op.ResourceSource != "" && op.ResourceSource != ep.ResourceSource {
		out = append(out, fmt.Sprintf("%s: x-resource-source %q != registry %q", key, op.ResourceSource, ep.ResourceSource))
	}
	if op.ResourceID != "" {
		from := "path"
		if strings.HasPrefix(op.ResourceID, "$.") {
			from = "body"
		}
		if ep.ResourceIDFrom != from || ep.ResourceID != op.ResourceID {
			out = append(out, fmt.Sprintf("%s: x-resource-id %q != registry %s:%q", key, op.ResourceID, ep.ResourceIDFrom, ep.ResourceID))
		}
	}
	method := strings.SplitN(key, " ", 2)[0]
	if ep.Action != "" || op.Action != "" {
		want := op.Action
		if want == "" {
			want = deriveAction(method)
		}
		if want != ep.Action {
			out = append(out, fmt.Sprintf("%s: x-action %q != registry %q", key, want, ep.Action))
		}
	}
	if ep.Attributes != nil || op.Attributes != nil {
		if !slices.Equal(op.Attributes, ep.Attributes) {
			out = append(out, fmt.Sprintf("%s: x-attributes %v != registry %v", key, op.Attributes, ep.Attributes))
		}
	}
	if ep.Freshness != "" || op.Freshness != "" {
		want := op.Freshness
		if want == "" {
			want = "cached"
		}
		if want != ep.Freshness {
			out = append(out, fmt.Sprintf("%s: x-freshness %q != registry %q", key, want, ep.Freshness))
		}
	}
	if ep.MaxStalenessSeconds != 0 || op.MaxStalenessSeconds != 0 {
		if op.MaxStalenessSeconds != ep.MaxStalenessSeconds {
			out = append(out, fmt.Sprintf("%s: x-max-staleness-seconds %d != registry %d", key, op.MaxStalenessSeconds, ep.MaxStalenessSeconds))
		}
	}
	if ep.SelfDescribing || op.SelfDescribing != nil {
		want := op.SelfDescribing != nil && *op.SelfDescribing
		if want != ep.SelfDescribing {
			out = append(out, fmt.Sprintf("%s: x-self-describing %t != registry %t", key, want, ep.SelfDescribing))
		}
	}
	if ep.MaskErrors != nil || op.MaskErrors != nil {
		want := true
		if op.MaskErrors != nil {
			want = *op.MaskErrors
		}
		got := true
		if ep.MaskErrors != nil {
			got = *ep.MaskErrors
		}
		if want != got {
			out = append(out, fmt.Sprintf("%s: x-mask-errors %t != registry %t", key, want, got))
		}
	}
	if ep.AllowNonJSON != nil || op.AllowNonJSON != nil {
		want := op.AllowNonJSON != nil && *op.AllowNonJSON
		got := ep.AllowNonJSON != nil && *ep.AllowNonJSON
		if want != got {
			out = append(out, fmt.Sprintf("%s: x-allow-non-json %t != registry %t", key, want, got))
		}
	}
	return out
}

func deriveAction(method string) string {
	switch method {
	case "GET", "HEAD":
		return "read"
	case "DELETE":
		return "delete"
	default:
		return "write"
	}
}

func registryByKey(endpoints []Endpoint) map[string]Endpoint {
	out := make(map[string]Endpoint, len(endpoints))
	for _, ep := range endpoints {
		out[opKey(strings.ToLower(ep.Method), ep.Path)] = ep
	}
	return out
}

func opKey(method, path string) string {
	return strings.ToUpper(method) + " " + Normalize(path)
}

// Normalize replaces path parameters with a placeholder so that
// /accounts/{accountId} and /accounts/{id} compare equal.
func Normalize(path string) string {
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			parts[i] = "{}"
		}
	}
	return strings.Join(parts, "/")
}
