// Package openapidiff compares a service's OpenAPI document with its endpoint
// registry. It is the gate that prevents an endpoint from shipping without a
// registry entry (uncontrolled) or a registry entry from going stale.
package openapidiff

import (
	"encoding/json"
	"sort"
	"strings"
)

// Document is the subset of an OpenAPI 3 document the gate needs.
type Document struct {
	Paths map[string]map[string]json.RawMessage `json:"paths"`
}

// Endpoint is a registry entry.
type Endpoint struct {
	ID     string
	Method string
	Path   string
}

var operations = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// Result is the set of differences.
type Result struct {
	MissingRegistry []string // documented operations with no registry entry
	OrphanRegistry  []string // registry entries with no documented operation
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
	for key := range documented {
		if !registered[key] {
			res.MissingRegistry = append(res.MissingRegistry, key)
		}
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
