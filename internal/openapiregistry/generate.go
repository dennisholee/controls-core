// Package openapiregistry derives the endpoint registry from OpenAPI 3
// operations and their x-resource-* extensions.
package openapiregistry

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/openapidiff"
)

type operation struct {
	OperationID    string   `json:"operationId"`
	ResourceType   string   `json:"x-resource-type"`
	ResourceID     string   `json:"x-resource-id"`
	MaskPolicy     string   `json:"x-mask-policy"`
	ResourceSource string   `json:"x-resource-source"`
	Action         string   `json:"x-action"`
	Attributes     []string `json:"x-attributes"`
	Freshness      string   `json:"x-freshness"`
	SelfDescribing *bool    `json:"x-self-describing"`
	MaskErrors     *bool    `json:"x-mask-errors"`
	MaxStale       int      `json:"x-max-staleness-seconds"`
	Ignore         bool     `json:"x-controls-ignore"`
}

var methods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

// Generate builds a registry from all annotated operations. Unannotated
// operations are skipped unless x-controls-ignore is set; annotated
// operations missing required bindings fail.
func Generate(doc openapidiff.Document, service string) (config.Registry, error) {
	reg := config.Registry{Version: "1.0", DefaultUnregistered: "deny"}
	for path, item := range doc.Paths {
		for method, raw := range item {
			m := strings.ToLower(method)
			if !methods[m] {
				continue
			}
			var op operation
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &op); err != nil {
					return reg, fmt.Errorf("%s %s: %w", method, path, err)
				}
			}
			if op.Ignore {
				continue
			}
			if op.ResourceType == "" && op.MaskPolicy == "" && op.ResourceID == "" {
				continue
			}
			if op.ResourceType == "" || op.MaskPolicy == "" || op.ResourceID == "" {
				return reg, fmt.Errorf("%s %s: x-resource-type, x-mask-policy and x-resource-id are required", method, path)
			}
			id := op.OperationID
			if id == "" {
				id = strings.ToLower(method) + strings.ReplaceAll(path, "/", "-")
			}
			ref := config.Ref{From: "path", Name: op.ResourceID}
			if strings.HasPrefix(op.ResourceID, "$.") {
				ref = config.Ref{From: "body", Path: op.ResourceID}
			}
			action := op.Action
			if action == "" {
				switch m {
				case "get", "head":
					action = "read"
				case "delete":
					action = "delete"
				default:
					action = "write"
				}
			}
			source := op.ResourceSource
			if source == "" {
				source = "pip"
			}
			freshness := op.Freshness
			if freshness == "" {
				freshness = "cached"
			}
			ep := config.Endpoint{
				ID:                  id,
				Service:             service,
				Method:              strings.ToUpper(m),
				Path:                path,
				ResourceType:        op.ResourceType,
				Action:              action,
				ResourceID:          ref,
				ResourceFrom:        source,
				Attributes:          op.Attributes,
				MaskPolicy:          op.MaskPolicy,
				MaskErrors:          op.MaskErrors,
				Freshness:           freshness,
				MaxStalenessSeconds: op.MaxStale,
			}
			if op.SelfDescribing != nil {
				ep.SelfDescribe = *op.SelfDescribing
			}
			reg.Endpoints = append(reg.Endpoints, ep)
		}
	}
	sort.Slice(reg.Endpoints, func(i, j int) bool { return reg.Endpoints[i].ID < reg.Endpoints[j].ID })
	return reg, nil
}
