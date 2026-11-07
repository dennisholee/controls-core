// Package engine orchestrates the per-request control pipeline:
//
//	registry match -> self-description cross-check -> resource resolution
//	-> policy decision -> data-quality evaluation -> response masking
//
// It is transport agnostic. The production adapter is an Envoy ext_proc
// service; the demo adapter in sidecar/cmd/sidecar is an HTTP harness used for
// tests and local development.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/dq"
	"example.com/controls/core/internal/mask"
	"example.com/controls/core/internal/pipclient"
	"example.com/controls/core/internal/policy"
)

// PIP is the subset of the PIP client the engine needs (mockable in tests).
type PIP interface {
	Resolve(ctx context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error)
	Classification(ctx context.Context, resourceType string) (map[string]string, error)
}

// freshPIP is implemented by clients that can bypass the local cache. The
// engine falls back to Resolve when a PIP does not support it.
type freshPIP interface {
	ResolveFresh(ctx context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error)
}

// defaultMaxStalenessSeconds applies to fresh/deny-on-stale endpoints that do
// not declare a bound.
const defaultMaxStalenessSeconds = 5

// Subject is the verified caller identity supplied by the gateway. The sidecar
// never derives this from a client-controlled header.
type Subject struct {
	Sub    string            `json:"sub"`
	Claims map[string]string `json:"claims,omitempty"`
}

// Request is the normalized input to the control plane.
type Request struct {
	RequestID string  `json:"requestId,omitempty"`
	Method    string  `json:"method"`
	Path      string  `json:"path"`
	Body      []byte  `json:"body,omitempty"`
	Subject   Subject `json:"subject"`
}

// Result is the outcome of request processing.
type Result struct {
	Allowed      bool           `json:"allowed"`
	Status       int            `json:"status"`
	Reason       string         `json:"reason"`
	EndpointID   string         `json:"endpointId,omitempty"`
	ResourceType string         `json:"resourceType,omitempty"`
	ResourceID   string         `json:"resourceId,omitempty"`
	Violations   []dq.Violation `json:"violations,omitempty"`
}

// Decision is emitted after every request for audit and metrics.
type Decision struct {
	RequestID     string
	EndpointID    string
	ResourceType  string
	ResourceID    string
	Subject       string
	Action        string
	Allowed       bool
	Status        int
	Reason        string
	Latency       time.Duration
	PIPCache      string
	PIPLagSeconds int
	Violations    []dq.Violation
}

// Hooks receive control-plane events. Set them before serving traffic.
type Hooks struct {
	OnDecision func(Decision)
}

// Engine evaluates controls for one request at a time. It is safe for
// concurrent use.
type Engine struct {
	cfg   *config.Bundle
	pol   policy.Evaluator
	dq    *dq.Evaluator
	pip   PIP
	hooks Hooks
}

// New builds an engine from a validated bundle.
func New(cfg *config.Bundle, pip PIP) *Engine {
	return &Engine{cfg: cfg, pol: policy.New(cfg), dq: dq.New(cfg), pip: pip}
}

// SetHooks installs audit/metrics hooks. Call once, before serving.
func (e *Engine) SetHooks(h Hooks) { e.hooks = h }

// ProcessRequest runs entitlement and data-quality checks.
func (e *Engine) ProcessRequest(ctx context.Context, req Request) (out Result) {
	start := time.Now()
	var endpoint *config.Endpoint
	var pipCache string
	var pipLag int
	defer func() {
		if e.hooks.OnDecision == nil {
			return
		}
		action := ""
		if endpoint != nil {
			action = actionOf(endpoint)
		}
		e.hooks.OnDecision(Decision{
			RequestID:     req.RequestID,
			EndpointID:    out.EndpointID,
			ResourceType:  out.ResourceType,
			ResourceID:    out.ResourceID,
			Subject:       req.Subject.Sub,
			Action:        action,
			Allowed:       out.Allowed,
			Status:        out.Status,
			Reason:        out.Reason,
			Latency:       time.Since(start),
			PIPCache:      pipCache,
			PIPLagSeconds: pipLag,
			Violations:    out.Violations,
		})
	}()

	ep, params := e.cfg.EndpointFor(req.Method, req.Path)
	endpoint = ep
	if ep == nil {
		if e.cfg.Registry.DefaultUnregistered == "allow" {
			return Result{Allowed: true, Status: 200, Reason: "unregistered:allow"}
		}
		return Result{Allowed: false, Status: 403, Reason: "unregistered_endpoint"}
	}

	var body map[string]any
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return Result{Allowed: false, Status: 400, Reason: "invalid_json", EndpointID: ep.ID}
		}
	}
	if body == nil {
		body = map[string]any{}
	}

	resourceID, ok := extractID(ep, params, body)
	if !ok {
		return Result{Allowed: false, Status: 400, Reason: "resource_id_missing", EndpointID: ep.ID}
	}

	if ep.SelfDescribe {
		if typ, present := body["_type"]; present && fmt.Sprint(typ) != ep.ResourceType {
			return Result{Allowed: false, Status: 403, Reason: "self_describing_type_mismatch",
				EndpointID: ep.ID, ResourceType: ep.ResourceType, ResourceID: resourceID}
		}
		if id, present := body["_id"]; present && fmt.Sprint(id) != resourceID {
			return Result{Allowed: false, Status: 403, Reason: "self_describing_id_mismatch",
				EndpointID: ep.ID, ResourceType: ep.ResourceType, ResourceID: resourceID}
		}
	}

	maskPolicy, _ := e.cfg.Masks.Policies[ep.MaskPolicy]
	for _, f := range maskPolicy.RejectFields {
		if _, present := lookupPath(body, f); present {
			return Result{Allowed: false, Status: 403, Reason: "rejected_field:" + f,
				EndpointID: ep.ID, ResourceType: ep.ResourceType, ResourceID: resourceID}
		}
	}

	resource := map[string]any{}
	source := ep.ResourceFrom
	if source == "" {
		source = "pip"
	}
	if source == "body" || source == "both" {
		for k, v := range body {
			resource[k] = v
		}
	}
	if source == "pip" || source == "both" {
		refs := []pipclient.Ref{{Type: ep.ResourceType, ID: resourceID, Fields: ep.Attributes}}
		freshness := ep.Freshness
		if freshness == "" {
			freshness = "cached"
		}
		var (
			attrs map[string]pipclient.Attribute
			err   error
		)
		if freshness == "fresh" {
			if fp, ok := e.pip.(freshPIP); ok {
				attrs, err = fp.ResolveFresh(ctx, refs)
			} else {
				attrs, err = e.pip.Resolve(ctx, refs)
			}
		} else {
			attrs, err = e.pip.Resolve(ctx, refs)
		}
		if err != nil {
			return Result{Allowed: false, Status: 503, Reason: "pip_unavailable",
				EndpointID: ep.ID, ResourceType: ep.ResourceType, ResourceID: resourceID}
		}
		attr, found := attrs[ep.ResourceType+"|"+resourceID]
		pipCache = attr.Cache
		pipLag = attr.LagSeconds
		if !found || !attr.Found {
			return Result{Allowed: false, Status: 404, Reason: "resource_not_found",
				EndpointID: ep.ID, ResourceType: ep.ResourceType, ResourceID: resourceID}
		}
		if freshness != "cached" {
			maxStale := ep.MaxStalenessSeconds
			if maxStale == 0 {
				maxStale = defaultMaxStalenessSeconds
			}
			if attr.LagSeconds > maxStale {
				return Result{Allowed: false, Status: 503, Reason: "stale_attributes",
					EndpointID: ep.ID, ResourceType: ep.ResourceType, ResourceID: resourceID}
			}
		}
		for k, v := range attr.Attributes {
			resource[k] = v
		}
	}

	result := e.pol.Decide(ep.ResourceType, actionOf(ep), policy.Context{
		Subject:  subjectMap(req.Subject),
		Resource: resource,
		Request:  map[string]any{"method": req.Method, "path": req.Path},
	})

	violations := e.dq.Check(ep.ResourceType, body)
	out = Result{
		Allowed:      result.Allowed,
		Status:       200,
		Reason:       result.Reason,
		EndpointID:   ep.ID,
		ResourceType: ep.ResourceType,
		ResourceID:   resourceID,
		Violations:   violations,
	}
	if !result.Allowed {
		out.Status = 403
		return out
	}
	for _, v := range violations {
		if v.Blocking {
			out.Allowed = false
			out.Status = 403
			out.Reason = "dq_blocking:" + v.RuleID
			return out
		}
	}
	return out
}

// MaskResponse applies the endpoint's mask policy to a JSON response body.
// Non-JSON bodies are returned unchanged and must be handled at the gateway.
func (e *Engine) MaskResponse(ep *config.Endpoint, body []byte, isError bool) ([]byte, error) {
	return e.MaskResponseCtx(context.Background(), ep, body, isError)
}

// MaskResponseCtx is MaskResponse with a context for classification lookups.
func (e *Engine) MaskResponseCtx(ctx context.Context, ep *config.Endpoint, body []byte, isError bool) ([]byte, error) {
	if ep == nil || ep.MaskPolicy == "" {
		return body, nil
	}
	if isError && !ep.MaskErrors {
		return body, nil
	}
	maskPolicy, ok := e.cfg.Masks.Policies[ep.MaskPolicy]
	if !ok {
		return body, nil
	}
	if len(body) == 0 {
		return body, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return body, nil
	}
	if maskPolicy.Fallback == "classification" {
		classes := map[string]string{}
		if cp, ok := e.pip.(interface {
			CachedClassification(string) (map[string]string, bool)
		}); ok {
			classes, _ = cp.CachedClassification(ep.ResourceType)
		}
		if len(classes) == 0 {
			fetched, err := e.pip.Classification(ctx, ep.ResourceType)
			if err != nil {
				return nil, fmt.Errorf("classification unavailable: %w", err)
			}
			classes = fetched
		}
		mask.ApplyWithClassification(maskPolicy, doc, classes)
	} else {
		mask.Apply(maskPolicy, doc)
	}
	return json.Marshal(doc)
}

// Endpoint exposes the matched endpoint for the response phase.
func (e *Engine) Endpoint(method, path string) *config.Endpoint {
	ep, _ := e.cfg.EndpointFor(method, path)
	return ep
}

// Bundle returns the loaded configuration (read-only).
func (e *Engine) Bundle() *config.Bundle { return e.cfg }

func actionOf(ep *config.Endpoint) string {
	if ep.Action != "" {
		return ep.Action
	}
	switch strings.ToUpper(ep.Method) {
	case "GET", "HEAD":
		return "read"
	case "POST", "PUT", "PATCH":
		return "write"
	case "DELETE":
		return "delete"
	}
	return strings.ToLower(ep.Method)
}

func subjectMap(s Subject) map[string]any {
	claims := map[string]any{}
	for k, v := range s.Claims {
		claims[k] = v
	}
	return map[string]any{"sub": s.Sub, "claims": claims}
}

func extractID(ep *config.Endpoint, params map[string]string, body map[string]any) (string, bool) {
	if ep.ResourceID.From == "path" {
		id, ok := params[ep.ResourceID.Name]
		return id, ok && id != ""
	}
	v, ok := lookupPath(body, strings.TrimPrefix(ep.ResourceID.Path, "$."))
	if !ok {
		return "", false
	}
	id := fmt.Sprint(v)
	return id, id != ""
}

func lookupPath(doc map[string]any, path string) (any, bool) {
	path = strings.TrimPrefix(path, "$.")
	var cur any = doc
	for _, p := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
