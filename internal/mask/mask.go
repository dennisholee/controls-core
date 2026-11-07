// Package mask applies field-level masking to response documents.
//
// Masking is the enforcement point for field entitlements: a mask policy is
// selected by the endpoint registry and applied to the serialized response.
// Modes are none, full and partial. Fields listed in config.LockedFields may
// only be full (validated at bundle load).
package mask

import (
	"fmt"
	"strings"

	"example.com/controls/core/internal/config"
)

// Apply masks doc in place according to policy and returns it.
func Apply(policy config.MaskPolicy, doc map[string]any) map[string]any {
	for path, field := range policy.Fields {
		applyPath(doc, strings.Split(path, "."), field, path)
	}
	if policy.Default == "full" {
		applyDefaultFull(doc, "", policy.Fields)
	}
	return doc
}

func applyDefaultFull(node map[string]any, prefix string, explicit map[string]config.MaskField) {
	for k, v := range node {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if isCovered(path, explicit) {
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			applyDefaultFull(child, path, explicit)
		case []any:
			for i := range child {
				if m, ok := child[i].(map[string]any); ok {
					applyDefaultFull(m, path+"[*]", explicit)
				} else {
					child[i] = fullValue(child[i])
				}
			}
		default:
			node[k] = fullValue(v)
		}
	}
}

func isCovered(path string, explicit map[string]config.MaskField) bool {
	if _, ok := explicit[path]; ok {
		return true
	}
	for p := range explicit {
		if strings.HasPrefix(path, strings.TrimSuffix(p, "[*]")+".") {
			return true
		}
	}
	return false
}

func applyPath(node any, parts []string, mf config.MaskField, fullPath string) {
	if len(parts) == 0 {
		return
	}
	key := parts[0]
	if strings.HasSuffix(key, "[*]") {
		parent, ok := node.(map[string]any)
		if !ok {
			return
		}
		k := strings.TrimSuffix(key, "[*]")
		arr, ok := parent[k].([]any)
		if !ok {
			return
		}
		for i := range arr {
			if len(parts) == 1 {
				arr[i] = maskValue(arr[i], mf)
			} else {
				applyPath(arr[i], parts[1:], mf, fullPath)
			}
		}
		return
	}
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if len(parts) == 1 {
		if _, exists := m[key]; exists {
			m[key] = maskValue(m[key], mf)
		}
		return
	}
	applyPath(m[key], parts[1:], mf, fullPath)
}

func maskValue(v any, mf config.MaskField) any {
	switch mf.Mode {
	case "", "none":
		return v
	case "full":
		return fullValue(v)
	case "partial":
		s, ok := v.(string)
		if !ok {
			s = fmt.Sprint(v)
		}
		return partial(s, mf)
	}
	return v
}

func fullValue(v any) any {
	if _, ok := v.(string); ok {
		return "[REDACTED]"
	}
	return nil
}

func partial(s string, mf config.MaskField) string {
	if mf.PreserveDomain {
		return maskEmail(s, mf)
	}
	if mf.Keep == "year" {
		if len(s) <= 4 {
			return strings.Repeat("*", len(s))
		}
		return s[:4] + strings.Repeat("*", len(s)-4)
	}
	prefix, suffix := mf.KeepPrefix, mf.KeepSuffix
	if prefix < 0 {
		prefix = 0
	}
	if suffix < 0 {
		suffix = 0
	}
	if prefix+suffix >= len(s) {
		return strings.Repeat("*", len(s))
	}
	return s[:prefix] + strings.Repeat("*", len(s)-prefix-suffix) + s[len(s)-suffix:]
}

func maskEmail(s string, mf config.MaskField) string {
	at := strings.LastIndex(s, "@")
	if at < 0 {
		return strings.Repeat("*", len(s))
	}
	local, domain := s[:at], s[at:]
	keep := mf.KeepPrefix
	if keep <= 0 {
		return "***" + domain
	}
	if keep >= len(local) {
		return local + domain
	}
	return local[:keep] + "***" + domain
}

// ApplyWithClassification applies explicit field rules and, when the policy
// opts in, masks every field the PIP classifies as PII that the policy did not
// explicitly cover. Explicit rules always win.
func ApplyWithClassification(policy config.MaskPolicy, doc map[string]any, classification map[string]string) map[string]any {
	Apply(policy, doc)
	if policy.Fallback != "classification" || len(classification) == 0 {
		return doc
	}
	applyClassification(doc, "", policy.Fields, classification)
	return doc
}

func applyClassification(node map[string]any, prefix string, explicit map[string]config.MaskField, classification map[string]string) {
	for k, v := range node {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if isCovered(path, explicit) {
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			applyClassification(child, path, explicit, classification)
		case []any:
			for i := range child {
				if m, ok := child[i].(map[string]any); ok {
					applyClassification(m, path+"[*]", explicit, classification)
				} else if isPII(path, k, classification) {
					child[i] = fullValue(child[i])
				}
			}
		default:
			if isPII(path, k, classification) {
				node[k] = fullValue(v)
			}
		}
	}
}

func isPII(path, leaf string, classification map[string]string) bool {
	tag, ok := classification[path]
	if !ok {
		tag, ok = classification[leaf]
	}
	return ok && strings.HasPrefix(tag, "pii.")
}
