// Package bundle renders delivery-owned configuration into one immutable,
// validated controls bundle per environment and service.
//
// It is the pipeline entry point described by ADR-0008: the central CI system
// orchestrates, but the merge semantics live here so they cannot drift from
// what the sidecar validates and loads.
package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"example.com/controls/core/internal/config"
)

// Options controls a render.
type Options struct {
	Src     string // source repo root (policies/, registry/, …)
	Out     string // output directory
	Env     string // optional environment overlay name
	Service string // optional service to render
	Check   bool   // validate only; do not write to Out
}

// Result describes the rendered bundle.
type Result struct {
	Dir      string         `json:"dir,omitempty"`
	Digest   string         `json:"digest"`
	Version  string         `json:"bundleVersion"`
	Counts   Counts         `json:"counts"`
	Sources  []string       `json:"sources"`
	Manifest map[string]any `json:"manifest,omitempty"`
}

// Counts summarises the bundle.
type Counts struct {
	Rules        int `json:"rules"`
	Endpoints    int `json:"endpoints"`
	MaskPolicies int `json:"maskPolicies"`
	DQRules      int `json:"dqRules"`
}

type rulesDoc struct {
	Version       string            `json:"version"`
	DefaultEffect string            `json:"defaultEffect"`
	Engine        string            `json:"engine,omitempty"`
	OPA           *config.OPAConfig `json:"opa,omitempty"`
	Rules         []config.Rule     `json:"rules"`
}

type masksDoc struct {
	Version  string                       `json:"version"`
	Policies map[string]config.MaskPolicy `json:"policies"`
}

type registryDoc struct {
	Version             string            `json:"version"`
	DefaultUnregistered string            `json:"defaultUnregistered"`
	Endpoints           []config.Endpoint `json:"endpoints"`
}

type dqDoc struct {
	Version string          `json:"version"`
	Rules   []config.DQRule `json:"rules"`
}

type overlay struct {
	Environment string `json:"environment"`
	Service     string `json:"service"`
	// Pipeline-owned sections are ignored by the renderer but accepted so a
	// single overlay can carry logging/signing settings.
	Controls json.RawMessage `json:"controls,omitempty"`
	Logging  json.RawMessage `json:"logging,omitempty"`
	Pipeline json.RawMessage `json:"pipeline,omitempty"`
	Bundle   struct {
		DefaultUnregistered string                       `json:"defaultUnregistered"`
		DisableRules        []string                     `json:"disableRules"`
		DisableDQRules      []string                     `json:"disableDQRules"`
		DisableEndpoints    []string                     `json:"disableEndpoints"`
		MaskPolicies        map[string]config.MaskPolicy `json:"maskPolicies"`
	} `json:"bundle"`
}

type manifest struct {
	SchemaVersion string   `json:"schemaVersion"`
	BundleDigest  string   `json:"bundleDigest"`
	BundleVersion string   `json:"bundleVersion"`
	Environment   string   `json:"environment,omitempty"`
	Service       string   `json:"service,omitempty"`
	GeneratedAt   string   `json:"generatedAt"`
	Counts        Counts   `json:"counts"`
	Sources       []string `json:"sources"`
}

// Render merges sources, applies env/service overlays, validates the result and
// writes the four bundle files plus a manifest. When Check is true it validates
// in a temporary directory and writes nothing.
func Render(opts Options) (*Result, error) {
	if opts.Src == "" {
		return nil, fmt.Errorf("src is required")
	}
	if !opts.Check && opts.Out == "" {
		return nil, fmt.Errorf("out is required unless -check")
	}
	src, err := filepath.Abs(opts.Src)
	if err != nil {
		return nil, err
	}

	reg, err := loadRegistry(src)
	if err != nil {
		return nil, err
	}
	rules, err := loadRules(src)
	if err != nil {
		return nil, err
	}
	masks, err := loadMasks(src)
	if err != nil {
		return nil, err
	}
	dq, err := loadDQ(src)
	if err != nil {
		return nil, err
	}
	cedar, err := loadCedar(src)
	if err != nil {
		return nil, err
	}
	if cedar != "" {
		if len(rules.Rules) > 0 {
			return nil, fmt.Errorf("cannot combine JSON rules with Cedar policies")
		}
		rules.Engine = "cedar"
	}

	sources := []string{}
	for _, dir := range []string{"policies", "mask-policies", "registry", "dq"} {
		files, _ := filepath.Glob(filepath.Join(src, dir, "*.json"))
		for _, f := range files {
			rel, _ := filepath.Rel(src, f)
			sources = append(sources, rel)
		}
	}
	if files, _ := filepath.Glob(filepath.Join(src, "policies", "*.cedar")); len(files) > 0 {
		for _, f := range files {
			rel, _ := filepath.Rel(src, f)
			sources = append(sources, rel)
		}
	}
	sort.Strings(sources)

	if opts.Env != "" {
		if err := applyOverlay(src, filepath.Join("env", opts.Env, "overrides.json"), reg, rules, masks, dq); err != nil {
			return nil, fmt.Errorf("env %q: %w", opts.Env, err)
		}
	}
	if opts.Service != "" {
		if err := applyOverlay(src, filepath.Join("services", opts.Service, "overrides.json"), reg, rules, masks, dq); err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("service %q: %w", opts.Service, err)
			}
		}
	}

	if opts.Service != "" {
		filterService(opts.Service, reg, masks)
		if len(reg.Endpoints) == 0 {
			return nil, fmt.Errorf("service %q has no registered endpoints", opts.Service)
		}
	}

	sort.Slice(rules.Rules, func(i, j int) bool { return rules.Rules[i].ID < rules.Rules[j].ID })
	sort.Slice(dq.Rules, func(i, j int) bool { return dq.Rules[i].ID < dq.Rules[j].ID })
	sort.Slice(reg.Endpoints, func(i, j int) bool { return reg.Endpoints[i].ID < reg.Endpoints[j].ID })

	contents, err := marshalBundle(reg, rules, masks, dq, cedar)
	if err != nil {
		return nil, err
	}
	digest := digestOf(contents)
	version := "bundle-" + strings.TrimPrefix(digest, "sha256:")[:12]

	res := &Result{
		Digest:  digest,
		Version: version,
		Sources: sources,
		Counts: Counts{
			Rules:        len(rules.Rules),
			Endpoints:    len(reg.Endpoints),
			MaskPolicies: len(masks.Policies),
			DQRules:      len(dq.Rules),
		},
	}

	if opts.Check {
		tmp, err := os.MkdirTemp("", "controls-bundle-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		if err := writeBundle(tmp, contents); err != nil {
			return nil, err
		}
		if _, err := config.LoadDir(tmp); err != nil {
			return nil, fmt.Errorf("rendered bundle is invalid: %w", err)
		}
		return res, nil
	}

	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return nil, err
	}
	if err := writeBundle(opts.Out, contents); err != nil {
		return nil, err
	}
	if _, err := config.LoadDir(opts.Out); err != nil {
		return nil, fmt.Errorf("rendered bundle is invalid: %w", err)
	}
	m := manifest{
		SchemaVersion: reg.Version,
		BundleDigest:  digest,
		BundleVersion: version,
		Environment:   opts.Env,
		Service:       opts.Service,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Counts:        res.Counts,
		Sources:       sources,
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(opts.Out, "manifest.json"), append(raw, '\n'), 0o644); err != nil {
		return nil, err
	}
	res.Dir = opts.Out
	res.Manifest = map[string]any{
		"bundleDigest":  m.BundleDigest,
		"bundleVersion": m.BundleVersion,
		"schemaVersion": m.SchemaVersion,
		"environment":   m.Environment,
		"service":       m.Service,
	}
	return res, nil
}

func loadRegistry(src string) (*registryDoc, error) {
	var out registryDoc
	seen := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(src, "registry", "*.json"))
	if len(files) == 0 {
		return nil, fmt.Errorf("no registry files under %s", src)
	}
	for _, f := range files {
		var doc registryDoc
		if err := readJSON(f, &doc); err != nil {
			return nil, err
		}
		if out.Version == "" {
			out.Version = doc.Version
		}
		if doc.DefaultUnregistered != "" {
			if out.DefaultUnregistered != "" && out.DefaultUnregistered != doc.DefaultUnregistered {
				return nil, fmt.Errorf("%s: conflicting defaultUnregistered", f)
			}
			out.DefaultUnregistered = doc.DefaultUnregistered
		}
		for _, ep := range doc.Endpoints {
			if seen[ep.ID] {
				return nil, fmt.Errorf("%s: duplicate endpoint id %q", f, ep.ID)
			}
			seen[ep.ID] = true
			out.Endpoints = append(out.Endpoints, ep)
		}
	}
	if out.Version == "" {
		out.Version = "1.0"
	}
	if out.DefaultUnregistered == "" {
		out.DefaultUnregistered = "deny"
	}
	return &out, nil
}

func loadRules(src string) (*rulesDoc, error) {
	out := &rulesDoc{Version: "1.0", DefaultEffect: "deny"}
	seen := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(src, "policies", "*.json"))
	for _, f := range files {
		var doc rulesDoc
		if err := readJSON(f, &doc); err != nil {
			return nil, err
		}
		if doc.DefaultEffect != "" {
			if out.DefaultEffect != "deny" && out.DefaultEffect != doc.DefaultEffect {
				return nil, fmt.Errorf("%s: conflicting defaultEffect", f)
			}
			out.DefaultEffect = doc.DefaultEffect
		}
		if doc.Engine != "" {
			if out.Engine != "" && out.Engine != doc.Engine {
				return nil, fmt.Errorf("%s: conflicting engine", f)
			}
			out.Engine = doc.Engine
		}
		if doc.OPA != nil {
			out.OPA = doc.OPA
		}
		for _, r := range doc.Rules {
			if seen[r.ID] {
				return nil, fmt.Errorf("%s: duplicate rule id %q", f, r.ID)
			}
			seen[r.ID] = true
			out.Rules = append(out.Rules, r)
		}
	}
	return out, nil
}

func loadMasks(src string) (*masksDoc, error) {
	out := &masksDoc{Version: "1.0", Policies: map[string]config.MaskPolicy{}}
	files, _ := filepath.Glob(filepath.Join(src, "mask-policies", "*.json"))
	for _, f := range files {
		var doc masksDoc
		if err := readJSON(f, &doc); err != nil {
			return nil, err
		}
		for name, p := range doc.Policies {
			if _, ok := out.Policies[name]; ok {
				return nil, fmt.Errorf("%s: duplicate mask policy %q", f, name)
			}
			out.Policies[name] = p
		}
	}
	return out, nil
}

func loadDQ(src string) (*dqDoc, error) {
	out := &dqDoc{Version: "1.0"}
	seen := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(src, "dq", "*.json"))
	for _, f := range files {
		var doc dqDoc
		if err := readJSON(f, &doc); err != nil {
			return nil, err
		}
		for _, r := range doc.Rules {
			if seen[r.ID] {
				return nil, fmt.Errorf("%s: duplicate dq rule id %q", f, r.ID)
			}
			seen[r.ID] = true
			out.Rules = append(out.Rules, r)
		}
	}
	return out, nil
}

func applyOverlay(src, rel string, reg *registryDoc, rules *rulesDoc, masks *masksDoc, dq *dqDoc) error {
	path := filepath.Join(src, rel)
	var ov overlay
	if err := readStrictJSON(path, &ov); err != nil {
		return err
	}
	if ov.Bundle.DefaultUnregistered != "" {
		if ov.Bundle.DefaultUnregistered == "allow" {
			return fmt.Errorf("defaultUnregistered: allow is not permitted in overlays")
		}
		reg.DefaultUnregistered = ov.Bundle.DefaultUnregistered
	}
	if len(ov.Bundle.DisableRules) > 0 {
		set := toSet(ov.Bundle.DisableRules)
		kept := rules.Rules[:0]
		for _, r := range rules.Rules {
			if set[r.ID] {
				delete(set, r.ID)
				continue
			}
			kept = append(kept, r)
		}
		if len(set) > 0 {
			return fmt.Errorf("disableRules references unknown ids: %v", keys(set))
		}
		rules.Rules = kept
	}
	if len(ov.Bundle.DisableDQRules) > 0 {
		set := toSet(ov.Bundle.DisableDQRules)
		kept := dq.Rules[:0]
		for _, r := range dq.Rules {
			if set[r.ID] {
				delete(set, r.ID)
				continue
			}
			kept = append(kept, r)
		}
		if len(set) > 0 {
			return fmt.Errorf("disableDQRules references unknown ids: %v", keys(set))
		}
		dq.Rules = kept
	}
	if len(ov.Bundle.DisableEndpoints) > 0 {
		set := toSet(ov.Bundle.DisableEndpoints)
		kept := reg.Endpoints[:0]
		for _, ep := range reg.Endpoints {
			if set[ep.ID] {
				delete(set, ep.ID)
				continue
			}
			kept = append(kept, ep)
		}
		if len(set) > 0 {
			return fmt.Errorf("disableEndpoints references unknown ids: %v", keys(set))
		}
		reg.Endpoints = kept
	}
	for name, p := range ov.Bundle.MaskPolicies {
		existing, ok := masks.Policies[name]
		if !ok {
			return fmt.Errorf("maskPolicies references unknown policy %q", name)
		}
		// Merge rather than replace: an overlay can add or change fields but
		// cannot delete an existing one (notably a locked field).
		if p.Default != "" {
			existing.Default = p.Default
		}
		if p.Fallback != "" {
			existing.Fallback = p.Fallback
		}
		if p.RejectFields != nil {
			existing.RejectFields = p.RejectFields
		}
		if len(p.Fields) > 0 {
			if existing.Fields == nil {
				existing.Fields = map[string]config.MaskField{}
			}
			for field, mf := range p.Fields {
				existing.Fields[field] = mf
			}
		}
		masks.Policies[name] = existing
	}
	return nil
}

func filterService(service string, reg *registryDoc, masks *masksDoc) {
	kept := reg.Endpoints[:0]
	used := map[string]bool{}
	for _, ep := range reg.Endpoints {
		if ep.Service != service {
			continue
		}
		kept = append(kept, ep)
		if ep.MaskPolicy != "" {
			used[ep.MaskPolicy] = true
		}
	}
	reg.Endpoints = kept
	for name := range masks.Policies {
		if !used[name] {
			delete(masks.Policies, name)
		}
	}
}

func marshalBundle(reg *registryDoc, rules *rulesDoc, masks *masksDoc, dq *dqDoc, cedar string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for name, v := range map[string]any{
		"rules.json":         rules,
		"mask-policies.json": masks,
		"registry.json":      reg,
		"dq.json":            dq,
	} {
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		out[name] = append(raw, '\n')
	}
	if cedar != "" {
		out["policies.cedar"] = []byte(cedar)
	}
	return out, nil
}

func writeBundle(dir string, contents map[string][]byte) error {
	for name, raw := range contents {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// DigestDir recomputes the bundle digest from the four files on disk. It must
// match the digest recorded by Render; signing and verification rely on it.
func DigestDir(dir string) (string, error) {
	contents := map[string][]byte{}
	for _, name := range []string{"rules.json", "mask-policies.json", "registry.json", "dq.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		contents[name] = raw
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "policies.cedar")); err == nil {
		contents["policies.cedar"] = raw
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return digestOf(contents), nil
}

func digestOf(contents map[string][]byte) string {
	h := sha256.New()
	for _, name := range []string{"rules.json", "mask-policies.json", "registry.json", "dq.json", "policies.cedar"} {
		raw, ok := contents[name]
		if !ok {
			continue
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(raw)
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func loadCedar(src string) (string, error) {
	files, _ := filepath.Glob(filepath.Join(src, "policies", "*.cedar"))
	if len(files) == 0 {
		return "", nil
	}
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return "", err
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.Write(raw)
		if !strings.HasSuffix(string(raw), "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

func readStrictJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, s := range items {
		out[s] = true
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
