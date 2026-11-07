package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OPAEvaluator delegates decisions to an external OPA data API.
//
// The sidecar supplies the base URL; the bundle supplies the decision path.
// Run OPA next to the sidecar (localhost or the mesh) — this evaluator does not
// cache, so every decision is an HTTP round trip.
type OPAEvaluator struct {
	url    string
	client *http.Client
}

// NewOPAEvaluator builds an evaluator for baseURL and a decision path such as
// "controls/authz/allow".
func NewOPAEvaluator(baseURL, decision string, timeout time.Duration) (*OPAEvaluator, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("opa: base URL is required")
	}
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	return &OPAEvaluator{
		url: strings.TrimRight(baseURL, "/") + "/v1/data/" + strings.TrimLeft(decision, "/"),
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Decide calls OPA with the policy context and fails closed on any error.
func (o *OPAEvaluator) Decide(_ string, action string, ctx Context) Result {
	payload, err := json.Marshal(map[string]any{
		"input": map[string]any{
			"subject":    ctx.Subject,
			"resource":   ctx.Resource,
			"resourceId": ctx.ResourceID,
			"action":     action,
			"request":    ctx.Request,
		},
	})
	if err != nil {
		return Result{Allowed: false, Reason: "opa:error"}
	}
	parent := ctx.Ctx
	if parent == nil {
		parent = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(parent, o.client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, o.url, bytes.NewReader(payload))
	if err != nil {
		return Result{Allowed: false, Reason: "opa:error"}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return Result{Allowed: false, Reason: "opa:error"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return Result{Allowed: false, Reason: "opa:error"}
	}
	var out struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return Result{Allowed: false, Reason: "opa:error"}
	}
	var allow bool
	if err := json.Unmarshal(out.Result, &allow); err == nil {
		if allow {
			return Result{Allowed: true, Reason: "opa:allow"}
		}
		return Result{Allowed: false, Reason: "opa:deny"}
	}
	var obj struct {
		Allow bool `json:"allow"`
	}
	if err := json.Unmarshal(out.Result, &obj); err == nil {
		if obj.Allow {
			return Result{Allowed: true, Reason: "opa:allow"}
		}
		return Result{Allowed: false, Reason: "opa:deny"}
	}
	return Result{Allowed: false, Reason: "opa:deny"}
}
