package policy

import (
	"fmt"
	"math"
	"strings"

	cedar "github.com/cedar-policy/cedar-go"
	cedartypes "github.com/cedar-policy/cedar-go/types"
)

// CedarEvaluator evaluates a Cedar policy set from the bundle.
//
// Mapping: principal is User::"<sub>" with claims as attributes (plus "sub");
// resource is <ResourceType>::"<id>" with its attribute map; action is
// Action::"<action>". Requests without a permit deny.
type CedarEvaluator struct {
	policies *cedar.PolicySet
}

// NewCedarEvaluator parses the policy document.
func NewCedarEvaluator(src string) (*CedarEvaluator, error) {
	ps, err := cedar.NewPolicySetFromBytes("policies.cedar", []byte(src))
	if err != nil {
		return nil, fmt.Errorf("parse cedar policies: %w", err)
	}
	return &CedarEvaluator{policies: ps}, nil
}

// Decide evaluates one request.
func (c *CedarEvaluator) Decide(resourceType, action string, ctx Context) Result {
	pType, err := cedartypes.NewEntityType("User")
	if err != nil {
		return Result{Allowed: false, Reason: "cedar:error"}
	}
	rType, err := cedartypes.NewEntityType(entityTypeName(resourceType))
	if err != nil {
		return Result{Allowed: false, Reason: "cedar:error"}
	}
	aType, err := cedartypes.NewEntityType("Action")
	if err != nil {
		return Result{Allowed: false, Reason: "cedar:error"}
	}
	sub := fmt.Sprint(ctx.Subject["sub"])
	resourceID := ctx.ResourceID
	if resourceID == "" {
		resourceID = "unknown"
	}
	pUID := cedar.NewEntityUID(pType, cedar.String(sub))
	rUID := cedar.NewEntityUID(rType, cedar.String(resourceID))
	aUID := cedar.NewEntityUID(aType, cedar.String(action))

	subjectAttrs := map[string]any{"sub": sub}
	if claims, ok := ctx.Subject["claims"].(map[string]any); ok {
		for k, v := range claims {
			subjectAttrs[k] = v
		}
	}
	entities := cedar.EntityMap{
		pUID: {UID: pUID, Attributes: toRecord(subjectAttrs)},
		rUID: {UID: rUID, Attributes: toRecord(ctx.Resource)},
	}
	reqCtx := cedar.RecordMap{}
	if v, ok := ctx.Request["method"].(string); ok && v != "" {
		reqCtx[cedar.String("method")] = cedar.String(v)
	}
	if v, ok := ctx.Request["path"].(string); ok && v != "" {
		reqCtx[cedar.String("path")] = cedar.String(v)
	}
	req := cedar.Request{
		Principal: pUID,
		Action:    aUID,
		Resource:  rUID,
		Context:   cedar.NewRecord(reqCtx),
	}
	decision, diag := c.policies.IsAuthorized(entities, req)
	if len(diag.Errors) > 0 {
		return Result{Allowed: false, Reason: "cedar:error"}
	}
	if bool(decision) {
		return Result{Allowed: true, Reason: "cedar:allow"}
	}
	return Result{Allowed: false, Reason: "cedar:deny"}
}

func entityTypeName(resourceType string) string {
	if resourceType == "" {
		return "Resource"
	}
	return strings.ToUpper(resourceType[:1]) + resourceType[1:]
}

func toRecord(m map[string]any) cedar.Record {
	rm := cedar.RecordMap{}
	for k, v := range m {
		if val, ok := cedarValue(v); ok {
			rm[cedar.String(k)] = val
		}
	}
	return cedar.NewRecord(rm)
}

func cedarValue(v any) (cedar.Value, bool) {
	switch x := v.(type) {
	case string:
		return cedar.String(x), true
	case bool:
		return cedar.Boolean(x), true
	case float64:
		if x == math.Trunc(x) {
			return cedar.Long(int64(x)), true
		}
		d, err := cedar.NewDecimalFromFloat(x)
		if err != nil {
			return nil, false
		}
		return d, true
	case int:
		return cedar.Long(x), true
	case int64:
		return cedar.Long(x), true
	case []any:
		vals := make([]cedar.Value, 0, len(x))
		for _, item := range x {
			if val, ok := cedarValue(item); ok {
				vals = append(vals, val)
			}
		}
		return cedar.NewSet(vals...), true
	case map[string]any:
		return toRecord(x), true
	}
	return nil, false
}
