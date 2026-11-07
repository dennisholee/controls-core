// Package openapidiff compares a service's OpenAPI document with its endpoint
// registry. It is the gate that prevents an endpoint from shipping without a
// registry entry (uncontrolled) or a registry entry from going stale.
package openapidiff

import (
	"encoding/json"
	"fmt"
	"os"
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
	ID             string
	Method         string
	Path           string
	ResourceType   string
	ResourceIDFrom string // path | body
	ResourceID     string // parameter name or $.-path
	MaskPolicy     string
	ResourceSource string
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
	ResourceType   string `json:"x-resource-type"`
	ResourceID     string `json:"x-resource-id"`
	MaskPolicy     string `json:"x-mask-policy"`
	ResourceSource string `json:"x-resource-source"`
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
	for path, item := range doc.Paths {
		for method := range item {
			m := strings.ToLower(method)
			if !operations[m] {
				continue
			}
			documented[opKey(m, path)] = true
		}
	}
	registered := map[string]bool{}
	for _, ep := range endpoints {
		registered[opKey(strings.ToLower(ep.Method), ep.Path)] = true
	}

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
			ops[opKey(m, path)] = op
		}
	}
	registeredByKey := registryByKey(endpoints)
	for key := range documented {
		if !registered[key] {
			res.MissingRegistry = append(res.MissingRegistry, key)
			continue
		}
		res.Mismatches = append(res.Mismatches, compareAnnotations(key, ops[key], registeredByKey[key])...)
	}
	for key, ep := range registryByKey(endpoints) {
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
	return out
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
