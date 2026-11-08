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
	Name           string                               `json:"name"`
	Service        string                               `json:"service,omitempty"`
	PIP            map[string]map[string]map[string]any `json:"pip,omitempty"`
	Classification map[string]string                    `json:"classification,omitempty"`
	Request        FixtureRequest                       `json:"request"`
	Response       *FixtureResponse                     `json:"response,omitempty"`
	Expect         FixtureExpect                        `json:"expect"`
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
	data     map[string]map[string]map[string]any
	classes  map[string]string
	fallback engine.PIP
}

func (p fixturePIP) Resolve(ctx context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	out := map[string]pipclient.Attribute{}
	for _, r := range refs {
		attrs, ok := p.data[r.Type][r.ID]
		if !ok && p.fallback != nil {
			// Fall through to the real PIP for resources the fixture does not
			// override, so a case only declares the data it cares about.
			fb, err := p.fallback.Resolve(ctx, []pipclient.Ref{r})
			if err != nil {
				return nil, err
			}
			if a, found := fb[r.Type+"|"+r.ID]; found {
				out[r.Type+"|"+r.ID] = a
				continue
			}
		}
		out[r.Type+"|"+r.ID] = pipclient.Attribute{
			Type: r.Type, ID: r.ID, Found: ok, Attributes: attrs,
		}
	}
	return out, nil
}

func (p fixturePIP) ResolveFresh(ctx context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	return p.Resolve(ctx, refs)
}

func (p fixturePIP) CachedClassification(string) (map[string]string, bool) { return nil, false }

func (p fixturePIP) Classification(ctx context.Context, resourceType string) (map[string]string, error) {
	if len(p.classes) > 0 {
		return p.classes, nil
	}
	if p.fallback == nil {
		return nil, nil
	}
	return p.fallback.Classification(ctx, resourceType)
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
		ev, evalErr := Evaluate(context.Background(), b, &f, nil)
		if evalErr != nil {
			res.Failed++
			res.Failures = append(res.Failures, fmt.Sprintf("%s: %v", f.Name, evalErr))
			if verbose {
				fmt.Printf("FAIL %s: %v\n", f.Name, evalErr)
			}
			continue
		}
		if !ev.Pass {
			res.Failed++
			res.Failures = append(res.Failures, fmt.Sprintf("%s: %s", f.Name, ev.Failures()))
			if verbose {
				fmt.Printf("FAIL %s: %s\n", f.Name, ev.Failures())
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

// Evaluation is the result of evaluating one fixture.
type Evaluation struct {
	Name     string             `json:"name"`
	Pass     bool               `json:"pass"`
	Decision engine.Result      `json:"decision"`
	Response *EvaluatedResponse `json:"response,omitempty"`
	Checks   []Check            `json:"checks"`
}

// EvaluatedResponse is the masked upstream response, when the fixture supplies one.
type EvaluatedResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// Check is one assertion made against a fixture.
type Check struct {
	Check string `json:"check"`
	OK    bool   `json:"ok"`
	Got   any    `json:"got"`
	Want  any    `json:"want"`
}

// Evaluate runs one fixture against b. When the fixture declares its own PIP
// data, that data overrides fallback; otherwise fallback (normally the live
// test API's PIP) is used.
func Evaluate(ctx context.Context, b *config.Bundle, f *Fixture, fallback engine.PIP) (*Evaluation, error) {
	var pip engine.PIP
	switch {
	case len(f.PIP) > 0:
		pip = fixturePIP{data: f.PIP, classes: f.Classification, fallback: fallback}
	case fallback != nil && len(f.Classification) == 0:
		pip = fallback
	default:
		pip = fixturePIP{classes: f.Classification, fallback: fallback}
	}
	eng := engine.New(b, pip)
	out := eng.ProcessRequest(ctx, engine.Request{
		Method:  f.Request.Method,
		Path:    f.Request.Path,
		Body:    f.Request.Body,
		Subject: subjectFromHeaders(f.Request.Headers),
	})
	ev := &Evaluation{Name: f.Name, Decision: out}
	ev.Checks = append(ev.Checks, Check{
		Check: "allowed", OK: out.Allowed == f.Expect.Allowed,
		Got: out.Allowed, Want: f.Expect.Allowed,
	})
	if f.Expect.Status != 0 {
		ev.Checks = append(ev.Checks, Check{
			Check: "status", OK: out.Status == f.Expect.Status,
			Got: out.Status, Want: f.Expect.Status,
		})
	}
	if f.Expect.Reason != "" {
		ev.Checks = append(ev.Checks, Check{
			Check: "reason", OK: out.Reason == f.Expect.Reason,
			Got: out.Reason, Want: f.Expect.Reason,
		})
	}
	if out.Allowed && f.Response != nil && len(f.Response.Body) > 0 {
		ep := eng.Endpoint(f.Request.Method, f.Request.Path)
		masked, err := eng.MaskResponseCtx(ctx, ep, f.Response.Body, f.Response.Status >= 400)
		if err != nil {
			return nil, err
		}
		ev.Response = &EvaluatedResponse{Status: f.Response.Status, Body: json.RawMessage(masked)}
		var doc map[string]any
		if err := json.Unmarshal(masked, &doc); err != nil {
			return nil, fmt.Errorf("masked response is not JSON: %w", err)
		}
		paths := make([]string, 0, len(f.Expect.Masked))
		for path := range f.Expect.Masked {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			want := f.Expect.Masked[path]
			got, ok := lookup(doc, path)
			ev.Checks = append(ev.Checks, Check{
				Check: "masked." + path,
				OK:    ok && fmt.Sprint(got) == fmt.Sprint(want),
				Got:   got,
				Want:  want,
			})
		}
	}
	ev.Pass = true
	for _, c := range ev.Checks {
		if !c.OK {
			ev.Pass = false
			break
		}
	}
	return ev, nil
}

// Failures renders the failed checks of an evaluation as one human-readable line.
func (e *Evaluation) Failures() string {
	var parts []string
	for _, c := range e.Checks {
		if c.OK {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s = %v, want %v", c.Check, c.Got, c.Want))
	}
	if len(parts) == 0 {
		return "fixture did not pass"
	}
	return strings.Join(parts, "; ")
}

// subjectFromHeaders derives the harness identity from x-subject/x-claim-*.
// Header names are matched case-insensitively because they arrive as JSON.
func subjectFromHeaders(headers map[string]string) engine.Subject {
	subject := engine.Subject{Claims: map[string]string{}}
	for k, v := range headers {
		lower := strings.ToLower(k)
		switch {
		case lower == "x-subject":
			subject.Sub = v
		case strings.HasPrefix(lower, "x-claim-"):
			subject.Claims[strings.TrimPrefix(lower, "x-claim-")] = v
		}
	}
	return subject
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
