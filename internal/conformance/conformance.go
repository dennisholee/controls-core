// Package conformance executes delivery-owned fixtures against a rendered
// bundle. It is the pipeline gate that proves a bundle behaves, not just that
// it validates.
package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/pipclient"
)

// Fixture is one acceptance case.
type Fixture struct {
	Name     string                               `json:"name"`
	Service  string                               `json:"service,omitempty"`
	PIP      map[string]map[string]map[string]any `json:"pip,omitempty"`
	Request  FixtureRequest                       `json:"request"`
	Response *FixtureResponse                     `json:"response,omitempty"`
	Expect   FixtureExpect                        `json:"expect"`
}

// FixtureRequest is the inbound request under test.
type FixtureRequest struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// FixtureResponse is the upstream response, when the case expects one.
type FixtureResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// FixtureExpect is the assertion.
type FixtureExpect struct {
	Allowed bool           `json:"allowed"`
	Status  int            `json:"status,omitempty"`
	Reason  string         `json:"reason,omitempty"`
	Masked  map[string]any `json:"masked,omitempty"`
}

// Result summarises a run.
type Result struct {
	Total    int      `json:"total"`
	Passed   int      `json:"passed"`
	Failed   int      `json:"failed"`
	Failures []string `json:"failures,omitempty"`
}

type fixturePIP struct {
	data map[string]map[string]map[string]any
}

func (p fixturePIP) Resolve(_ context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	out := map[string]pipclient.Attribute{}
	for _, r := range refs {
		attrs, ok := p.data[r.Type][r.ID]
		out[r.Type+"|"+r.ID] = pipclient.Attribute{
			Type: r.Type, ID: r.ID, Found: ok, Attributes: attrs,
		}
	}
	return out, nil
}

func (p fixturePIP) Classification(context.Context, string) (map[string]string, error) {
	return nil, nil
}

// Run loads a bundle and every fixture in dir, then asserts each case.
func Run(bundleDir, fixturesDir, service string, verbose bool) (*Result, error) {
	b, err := config.LoadDir(bundleDir)
	if err != nil {
		return nil, fmt.Errorf("load bundle: %w", err)
	}
	files, err := filepath.Glob(filepath.Join(fixturesDir, "*.json"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no fixtures under %s", fixturesDir)
	}
	sort.Strings(files)

	res := &Result{}
	for _, path := range files {
		var f Fixture
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if service != "" && f.Service != "" && f.Service != service {
			continue
		}
		res.Total++
		if err := runOne(b, &f); err != nil {
			res.Failed++
			res.Failures = append(res.Failures, fmt.Sprintf("%s: %v", f.Name, err))
			if verbose {
				fmt.Printf("FAIL %s: %v\n", f.Name, err)
			}
			continue
		}
		res.Passed++
		if verbose {
			fmt.Printf("PASS %s\n", f.Name)
		}
	}
	return res, nil
}

func runOne(b *config.Bundle, f *Fixture) error {
	eng := engine.New(b, fixturePIP{data: f.PIP})
	subject := engine.Subject{Sub: f.Request.Headers["x-subject"], Claims: map[string]string{}}
	for k, v := range f.Request.Headers {
		if strings.HasPrefix(k, "x-claim-") {
			subject.Claims[strings.TrimPrefix(k, "x-claim-")] = v
		}
	}
	out := eng.ProcessRequest(context.Background(), engine.Request{
		Method:  f.Request.Method,
		Path:    f.Request.Path,
		Body:    f.Request.Body,
		Subject: subject,
	})
	if out.Allowed != f.Expect.Allowed {
		return fmt.Errorf("allowed = %v, want %v (status %d, reason %s)", out.Allowed, f.Expect.Allowed, out.Status, out.Reason)
	}
	if f.Expect.Status != 0 && out.Status != f.Expect.Status {
		return fmt.Errorf("status = %d, want %d (reason %s)", out.Status, f.Expect.Status, out.Reason)
	}
	if f.Expect.Reason != "" && out.Reason != f.Expect.Reason {
		return fmt.Errorf("reason = %q, want %q", out.Reason, f.Expect.Reason)
	}
	if !f.Expect.Allowed || f.Response == nil || len(f.Response.Body) == 0 {
		return nil
	}
	ep := eng.Endpoint(f.Request.Method, f.Request.Path)
	masked, err := eng.MaskResponse(ep, f.Response.Body, f.Response.Status >= 400)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(masked, &doc); err != nil {
		return fmt.Errorf("masked response is not JSON: %w", err)
	}
	for path, want := range f.Expect.Masked {
		got, ok := lookup(doc, path)
		if !ok {
			return fmt.Errorf("masked path %q missing", path)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Errorf("masked %q = %v, want %v", path, got, want)
		}
	}
	return nil
}

func lookup(doc map[string]any, path string) (any, bool) {
	var cur any = doc
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
