package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Repository is the read-only data port. The reference implementation is an
// in-memory fixture; the production implementation is a set of per-domain
// adapters over read replicas with curated, parameterized queries.

type Repository interface {
	Get(ctx context.Context, resourceType, id string, fields []string) (map[string]any, bool, error)
	Classification(ctx context.Context, resourceType string) (map[string]string, bool, error)
}

// MemoryRepo serves fixtures. It exists so the sidecar and the PIP contract can
// be developed and tested without infrastructure.
type MemoryRepo struct {
	resources       map[string]map[string]map[string]any
	classifications map[string]map[string]string
}

// LoadMemoryRepo reads a fixture file of the form:
//
//	{"resources":{"account":{"A123":{...}}},"classifications":{"account":{...}}}
func LoadMemoryRepo(path string) (*MemoryRepo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Resources       map[string]map[string]map[string]any `json:"resources"`
		Classifications map[string]map[string]string         `json:"classifications"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &MemoryRepo{resources: raw.Resources, classifications: raw.Classifications}, nil
}

// Get returns a projected copy of the resource attributes.
func (r *MemoryRepo) Get(_ context.Context, resourceType, id string, fields []string) (map[string]any, bool, error) {
	byID, ok := r.resources[resourceType]
	if !ok {
		return nil, false, nil
	}
	attrs, ok := byID[id]
	if !ok {
		return nil, false, nil
	}
	if len(fields) == 0 {
		out := make(map[string]any, len(attrs))
		for k, v := range attrs {
			out[k] = v
		}
		return out, true, nil
	}
	out := map[string]any{}
	for _, f := range fields {
		if v, ok := attrs[f]; ok {
			out[f] = v
		}
	}
	return out, true, nil
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
