package testapi

import (
	"context"

	"example.com/controls/core/internal/pipclient"
	"example.com/controls/core/pip/server"
)

// LocalPIP adapts the reference in-memory PIP fixtures to engine.PIP. It lets
// the test API run as a single process with no external PIP.
type LocalPIP struct {
	repo *server.MemoryRepo
}

// NewLocalPIP loads a fixtures file of the form
// {"resources":{...},"classifications":{...}}.
func NewLocalPIP(path string) (*LocalPIP, error) {
	repo, err := server.LoadMemoryRepo(path)
	if err != nil {
		return nil, err
	}
	return &LocalPIP{repo: repo}, nil
}

// Resolve returns the fixture attributes for each reference.
func (p *LocalPIP) Resolve(ctx context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	out := make(map[string]pipclient.Attribute, len(refs))
	for _, ref := range refs {
		res, err := p.repo.Get(ctx, ref.Type, ref.ID, ref.Fields)
		if err != nil {
			return nil, err
		}
		out[ref.Type+"|"+ref.ID] = pipclient.Attribute{
			Type:       ref.Type,
			ID:         ref.ID,
			Found:      res.Found,
			Attributes: res.Attributes,
			Version:    res.Version,
			LagSeconds: res.LagSeconds,
			Cache:      "miss",
		}
	}
	return out, nil
}

// ResolveFresh is the same as Resolve: the fixture repository has no cache.
func (p *LocalPIP) ResolveFresh(ctx context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	return p.Resolve(ctx, refs)
}

// Classification returns the fixture classification tags.
func (p *LocalPIP) Classification(ctx context.Context, resourceType string) (map[string]string, error) {
	fields, ok, err := p.repo.Classification(ctx, resourceType)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]string{}, nil
	}
	return fields, nil
}
