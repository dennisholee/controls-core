package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
)

// Repository is the read-only data port. The reference implementation is an
// in-memory fixture; the production implementation is a set of per-domain
// adapters over read replicas with curated, parameterized queries.

type Repository interface {
	Get(ctx context.Context, resourceType, id string, fields []string) (Resource, error)
	Classification(ctx context.Context, resourceType string) (map[string]string, bool, error)
}

// ErrForbidden means the authenticated caller may not read the requested
// resource or field.
var ErrForbidden = errors.New("caller not authorized")

// Resource is one resolved attribute set. LagSeconds is the replica lag
// reported by the source (zero when unknown).
type Resource struct {
	Attributes map[string]any
	Found      bool
	LagSeconds int
	LagKnown   bool
	Version    string
	TTLSeconds int
}

// resourceMeta is optional per-resource fixture metadata. It lets a local
// fixture exercise stale/unknown lag and cache TTL without a custom PIP.
type resourceMeta struct {
	LagSeconds int    `json:"lagSeconds"`
	LagKnown   *bool  `json:"lagKnown"`
	Version    string `json:"version"`
	TTLSeconds int    `json:"ttlSeconds"`
}

// MemoryRepo serves fixtures. It exists so the sidecar and the PIP contract can
// be developed and tested without infrastructure.
type MemoryRepo struct {
	resources       map[string]map[string]map[string]any
	classifications map[string]map[string]string
	meta            map[string]map[string]resourceMeta
}

// LoadMemoryRepo reads a fixture file of the form:
//
//	{"resources":{"account":{"A123":{...}}},"classifications":{"account":{...}},
//	 "resourceMeta":{"account":{"A123":{"lagSeconds":6,"lagKnown":true}}}}
func LoadMemoryRepo(path string) (*MemoryRepo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Resources       map[string]map[string]map[string]any `json:"resources"`
		Classifications map[string]map[string]string         `json:"classifications"`
		ResourceMeta    map[string]map[string]resourceMeta   `json:"resourceMeta"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &MemoryRepo{
		resources:       raw.Resources,
		classifications: raw.Classifications,
		meta:            raw.ResourceMeta,
	}, nil
}

// Get returns a projected copy of the resource attributes.
func (r *MemoryRepo) Get(_ context.Context, resourceType, id string, fields []string) (Resource, error) {
	byID, ok := r.resources[resourceType]
	if !ok {
		return Resource{Found: false}, nil
	}
	attrs, ok := byID[id]
	if !ok {
		return Resource{Found: false}, nil
	}
	out := map[string]any{}
	if len(fields) == 0 {
		for k, v := range attrs {
			out[k] = v
		}
	} else {
		for _, f := range fields {
			if v, ok := attrs[f]; ok {
				out[f] = v
			}
		}
	}
	res := Resource{Attributes: out, Found: true, Version: "fixture-v1"}
	if byType, ok := r.meta[resourceType]; ok {
		if m, ok := byType[id]; ok {
			res.LagSeconds = m.LagSeconds
			res.TTLSeconds = m.TTLSeconds
			if m.LagKnown != nil {
				res.LagKnown = *m.LagKnown
			}
			if m.Version != "" {
				res.Version = m.Version
			}
		}
	}
	return res, nil
}

// Classification returns the field classification map.
func (r *MemoryRepo) Classification(_ context.Context, resourceType string) (map[string]string, bool, error) {
	fields, ok := r.classifications[resourceType]
	if !ok {
		return nil, false, nil
	}
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	return out, true, nil
}

// ResourceTypes returns known types in deterministic order (startup logging).
func (r *MemoryRepo) ResourceTypes() []string {
	out := make([]string, 0, len(r.resources))
	for t := range r.resources {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
