// Package controlscheck cross-checks a bundle against the OpenAPI contract:
// resource types, actions, mask paths, DQ paths and PIP attributes.
package controlscheck

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/openapidiff"
)

// Result is the validation outcome.
type Result struct {
	Errors   []string
	Warnings []string
}

// Validate checks rules, masks and DQ against the registry and OpenAPI schemas.
func Validate(b *config.Bundle, doc *openapidiff.Document, pipFixture map[string]any) Result {
	var res Result
	types := map[string]bool{}
	actions := map[string]map[string]bool{}
	endpointsByType := map[string][]config.Endpoint{}
	for _, ep := range b.Registry.Endpoints {
		types[ep.ResourceType] = true
		if actions[ep.ResourceType] == nil {
			actions[ep.ResourceType] = map[string]bool{}
		}
		actions[ep.ResourceType][ep.Action] = true
		endpointsByType[ep.ResourceType] = append(endpointsByType[ep.ResourceType], ep)
	}

	for _, r := range b.Rules.Rules {
		if r.ResourceType != "*" && r.ResourceType != "" && !types[r.ResourceType] {
			res.Errors = append(res.Errors, fmt.Sprintf("rule %q targets unknown resourceType %q", r.ID, r.ResourceType))
		}
		if r.Action != "" && r.Action != "*" {
			if r.ResourceType == "*" || r.ResourceType == "" {
				if !anyAction(actions, r.Action) {
					res.Errors = append(res.Errors, fmt.Sprintf("rule %q targets unknown action %q", r.ID, r.Action))
				}
			} else if !actions[r.ResourceType][r.Action] {
				res.Errors = append(res.Errors, fmt.Sprintf("rule %q targets action %q not registered for %q", r.ID, r.Action, r.ResourceType))
			}
		}
	}

	for _, r := range b.DQ.Rules {
		if r.ResourceType != "*" && r.ResourceType != "" && !types[r.ResourceType] {
			res.Errors = append(res.Errors, fmt.Sprintf("dq rule %q targets unknown resourceType %q", r.ID, r.ResourceType))
			continue
		}
		if r.ResourceType == "*" || r.ResourceType == "" {
			continue
		}
		schema, ok := requestSchema(doc, endpointsByType[r.ResourceType])
		if !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("dq rule %q: no JSON request schema for %q", r.ID, r.ResourceType))
			continue
		}
		if !pathExists(doc, schema, r.Field) {
			res.Errors = append(res.Errors, fmt.Sprintf("dq rule %q field %q not in request schema", r.ID, r.Field))
		}
	}

	used := map[string]bool{}
	for _, ep := range b.Registry.Endpoints {
		if ep.MaskPolicy == "" {
			continue
		}
		used[ep.MaskPolicy] = true
		policy, ok := b.Masks.Policies[ep.MaskPolicy]
		if !ok {
			continue // config.Validate already reports this
		}
		schema, ok := responseSchema(doc, ep)
		if !ok {
			res.Warnings = append(res.Warnings, fmt.Sprintf("endpoint %q: no JSON response schema for mask policy %q", ep.ID, ep.MaskPolicy))
			continue
		}
		for field := range policy.Fields {
			if !pathExists(doc, schema, field) {
				res.Warnings = append(res.Warnings, fmt.Sprintf("mask policy %q field %q not in response schema for %q", ep.MaskPolicy, field, ep.ID))
			}
		}
		if len(policy.RejectFields) > 0 {
			req, ok := requestSchema(doc, []config.Endpoint{ep})
			if !ok {
				res.Warnings = append(res.Warnings, fmt.Sprintf("endpoint %q: rejectFields without a JSON request schema", ep.ID))
			} else {
				for _, field := range policy.RejectFields {
					if !pathExists(doc, req, field) {
						res.Errors = append(res.Errors, fmt.Sprintf("mask policy %q reject field %q not in request schema for %q", ep.MaskPolicy, field, ep.ID))
					}
				}
			}
		}
	}
	var names []string
	for name := range b.Masks.Policies {
		if !used[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		res.Warnings = append(res.Warnings, fmt.Sprintf("mask policy %q is not referenced by any endpoint", name))
	}

	if pipFixture != nil {
		for _, ep := range b.Registry.Endpoints {
			if ep.ResourceFrom != "pip" && ep.ResourceFrom != "both" {
				continue
			}
			for _, attr := range ep.Attributes {
				if !fixtureHas(pipFixture, ep.ResourceType, attr) {
					res.Errors = append(res.Errors, fmt.Sprintf("endpoint %q attribute %q missing from PIP fixture for %q", ep.ID, attr, ep.ResourceType))
				}
			}
		}
	}
	return res
}

func anyAction(actions map[string]map[string]bool, action string) bool {
	for _, set := range actions {
		if set[action] {
			return true
		}
	}
	return false
}

func fixtureHas(fixture map[string]any, resourceType, field string) bool {
	resources, _ := fixture["resources"].(map[string]any)
	byID, _ := resources[resourceType].(map[string]any)
	for _, v := range byID {
		attrs, _ := v.(map[string]any)
		if _, ok := attrs[field]; ok {
			return true
		}
	}
	return false
}

type operation struct {
	RequestBody *struct {
		Content map[string]struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"content"`
	} `json:"requestBody"`
	Responses map[string]struct {
		Content map[string]struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"content"`
	} `json:"responses"`
}

func docOperation(doc *openapidiff.Document, ep config.Endpoint) (operation, bool) {
	item, ok := doc.Paths[ep.Path]
	if !ok {
		return operation{}, false
	}
	raw, ok := item[strings.ToLower(ep.Method)]
	if !ok {
		return operation{}, false
	}
	var op operation
	if err := json.Unmarshal(raw, &op); err != nil {
		return operation{}, false
	}
	return op, true
}

func responseSchema(doc *openapidiff.Document, ep config.Endpoint) (json.RawMessage, bool) {
	op, ok := docOperation(doc, ep)
	if !ok {
		return nil, false
	}
	for _, status := range []string{"200", "201", "202"} {
		if r, ok := op.Responses[status]; ok {
			if media, ok := r.Content["application/json"]; ok {
				return media.Schema, true
			}
		}
	}
	return nil, false
}

func requestSchema(doc *openapidiff.Document, eps []config.Endpoint) (json.RawMessage, bool) {
	for _, ep := range eps {
		op, ok := docOperation(doc, ep)
		if !ok || op.RequestBody == nil {
			continue
		}
		if media, ok := op.RequestBody.Content["application/json"]; ok {
			return media.Schema, true
		}
	}
	return nil, false
}

func pathExists(doc *openapidiff.Document, raw json.RawMessage, path string) bool {
	var cur any
	if err := json.Unmarshal(raw, &cur); err != nil {
		return false
	}
	for _, seg := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		arr := strings.HasSuffix(seg, "[*]")
		name := strings.TrimSuffix(seg, "[*]")
		m := resolve(doc, cur)
		obj, ok := m.(map[string]any)
		if !ok {
			return false
		}
		props, _ := obj["properties"].(map[string]any)
		next, ok := props[name]
		if !ok {
			return false
		}
		cur = next
		if arr {
			arrSchema := resolve(doc, cur)
			o, ok := arrSchema.(map[string]any)
			if !ok {
				return false
			}
			items, ok := o["items"]
			if !ok {
				return false
			}
			cur = items
		}
	}
	return true
}

func resolve(doc *openapidiff.Document, v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if ref, ok := m["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if raw, ok := doc.Components.Schemas[name]; ok {
			var out any
			if json.Unmarshal(raw, &out) == nil {
				return out
			}
		}
	}
	return m
}
