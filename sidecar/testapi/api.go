// Package testapi exposes the controls engine over a local HTTP API.
//
// It exists so the solution can be exercised end to end without Envoy: post a
// request envelope and get the decision plus the masked response, or post a
// conformance fixture and get pass/fail checks. The API is a test harness, not
// a production entrypoint. It trusts x-subject/x-claim-* headers, so it must
// never be exposed outside a developer machine or a CI network.
package testapi

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"example.com/controls/core/contracts"
	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/conformance"
	"example.com/controls/core/internal/engine"
)

// DefaultMaxBodyBytes bounds one request body.
const DefaultMaxBodyBytes = 1 << 20

//go:embed console.html
var consoleHTML []byte

// Envelope is the request under test plus the upstream response to mask.
// It matches the conformance fixture shape so a fixture file can be posted to
// /v1/test unchanged.
type Envelope struct {
	Request  Request   `json:"request"`
	Response *Response `json:"response,omitempty"`
	// Subject overrides the x-subject/x-claim-* headers when supplied.
	Subject *Subject `json:"subject,omitempty"`
}

// Request is the inbound request under test.
type Request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// Response is the upstream response whose body is masked when allowed.
type Response struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// Subject is the harness identity. Production identities come from the
// gateway trust chain instead.
type Subject struct {
	Sub    string            `json:"sub"`
	Claims map[string]string `json:"claims,omitempty"`
}

// ProcessResult is returned by POST /v1/process.
type ProcessResult struct {
	Decision engine.Result `json:"decision"`
	Response *Response     `json:"response,omitempty"`
}

// Option configures the API.
type Option func(*API)

// WithBundleDir records the bundle directory for /v1/info.
func WithBundleDir(dir string) Option { return func(a *API) { a.bundleDir = dir } }

// WithPIPMode records how attributes are resolved ("fixtures" or "http").
func WithPIPMode(mode string) Option { return func(a *API) { a.pipMode = mode } }

// WithMaxBodyBytes bounds request bodies.
func WithMaxBodyBytes(n int64) Option {
	return func(a *API) {
		if n > 0 {
			a.maxBody = n
		}
	}
}

// API serves the controls test interface.
type API struct {
	bundle    *config.Bundle
	eng       *engine.Engine
	pip       engine.PIP
	log       *slog.Logger
	bundleDir string
	pipMode   string
	maxBody   int64
	started   time.Time
	handler   http.Handler
}

// New builds the test API. A policy-engine initialisation failure is reported
// by StartupError and requests fail closed.
func New(b *config.Bundle, pip engine.PIP, log *slog.Logger, opts ...Option) *API {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	a := &API{
		bundle:  b,
		pip:     pip,
		log:     log,
		pipMode: "http",
		maxBody: DefaultMaxBodyBytes,
		started: time.Now(),
	}
	for _, opt := range opts {
		opt(a)
	}
	a.eng = engine.New(b, pip)
	a.eng.SetHooks(engine.Hooks{OnDecision: func(d engine.Decision) {
		log.Info("decision",
			"endpoint", d.EndpointID,
			"resource", d.ResourceType+"/"+d.ResourceID,
			"allowed", d.Allowed,
			"status", d.Status,
			"reason", d.Reason,
			"violations", len(d.Violations),
			"subject", d.Subject,
			"latency_ms", float64(d.Latency.Microseconds())/1000,
		)
	}})
	a.handler = a.routes()
	return a
}

// StartupError reports a policy engine that could not be initialised.
func (a *API) StartupError() error { return a.eng.StartupError() }

// Handler returns the HTTP handler.
func (a *API) Handler() http.Handler { return a.handler }

func (a *API) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", a.handleReady)
	mux.HandleFunc("GET /v1/info", a.handleInfo)
	mux.HandleFunc("GET /v1/endpoints", a.handleEndpoints)
	mux.HandleFunc("POST /v1/process", a.handleProcess)
	mux.HandleFunc("POST /v1/test", a.handleTest)
	mux.HandleFunc("GET /openapi.yaml", a.handleOpenAPI)
	mux.HandleFunc("GET /docs", a.handleDocs)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusFound)
	})
	return a.withHeaders(a.withLogging(mux))
}

func (a *API) handleReady(w http.ResponseWriter, _ *http.Request) {
	if err := a.StartupError(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "policy_init_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

type bundleInfo struct {
	Dir          string `json:"dir,omitempty"`
	Version      string `json:"version,omitempty"`
	Environment  string `json:"environment,omitempty"`
	Service      string `json:"service,omitempty"`
	Digest       string `json:"digest,omitempty"`
	PolicyEngine string `json:"policyEngine"`
	Endpoints    int    `json:"endpoints"`
	Rules        int    `json:"rules"`
	MaskPolicies int    `json:"maskPolicies"`
	DQRules      int    `json:"dqRules"`
}

type pipInfo struct {
	Mode string `json:"mode"`
}

type infoResponse struct {
	API        string     `json:"api"`
	APIVersion string     `json:"apiVersion"`
	Bundle     bundleInfo `json:"bundle"`
	PIP        pipInfo    `json:"pip"`
	StartedAt  time.Time  `json:"startedAt"`
}

func (a *API) handleInfo(w http.ResponseWriter, _ *http.Request) {
	bi := bundleInfo{
		Dir:          a.bundleDir,
		PolicyEngine: a.bundle.Rules.Engine,
		Endpoints:    len(a.bundle.Registry.Endpoints),
		Rules:        len(a.bundle.Rules.Rules),
		MaskPolicies: len(a.bundle.Masks.Policies),
		DQRules:      len(a.bundle.DQ.Rules),
	}
	if bi.PolicyEngine == "" {
		bi.PolicyEngine = "rules"
	}
	bi.Version = a.bundle.Registry.Version
	if m := a.bundle.Manifest; m != nil {
		bi.Version = m.BundleVersion
		bi.Environment = m.Environment
		bi.Service = m.Service
		bi.Digest = m.BundleDigest
	}
	writeJSON(w, http.StatusOK, infoResponse{
		API:        "controls-test-api",
		APIVersion: "v1",
		Bundle:     bi,
		PIP:        pipInfo{Mode: a.pipMode},
		StartedAt:  a.started,
	})
}

type maskView struct {
	Default      string                      `json:"default"`
	Fallback     string                      `json:"fallback,omitempty"`
	Fields       map[string]config.MaskField `json:"fields,omitempty"`
	RejectFields []string                    `json:"rejectFields,omitempty"`
}

type endpointView struct {
	ID                  string     `json:"id"`
	Method              string     `json:"method"`
	Path                string     `json:"path"`
	ResourceType        string     `json:"resourceType"`
	Action              string     `json:"action"`
	ResourceID          config.Ref `json:"resourceId"`
	ResourceSource      string     `json:"resourceSource"`
	Attributes          []string   `json:"attributes,omitempty"`
	MaskPolicy          string     `json:"maskPolicy,omitempty"`
	Mask                *maskView  `json:"mask,omitempty"`
	Freshness           string     `json:"freshness"`
	MaxStalenessSeconds int        `json:"maxStalenessSeconds,omitempty"`
	SelfDescribing      bool       `json:"selfDescribing"`
}

type endpointsResponse struct {
	DefaultUnregistered string         `json:"defaultUnregistered"`
	Endpoints           []endpointView `json:"endpoints"`
}

func (a *API) handleEndpoints(w http.ResponseWriter, _ *http.Request) {
	resp := endpointsResponse{
		DefaultUnregistered: a.bundle.Registry.DefaultUnregistered,
		Endpoints:           make([]endpointView, 0, len(a.bundle.Registry.Endpoints)),
	}
	for _, ep := range a.bundle.Registry.Endpoints {
		source := ep.ResourceFrom
		if source == "" {
			source = "pip"
		}
		freshness := ep.Freshness
		if freshness == "" {
			freshness = "cached"
		}
		v := endpointView{
			ID:                  ep.ID,
			Method:              ep.Method,
			Path:                ep.Path,
			ResourceType:        ep.ResourceType,
			Action:              ep.Action,
			ResourceID:          ep.ResourceID,
			ResourceSource:      source,
			Attributes:          ep.Attributes,
			MaskPolicy:          ep.MaskPolicy,
			Freshness:           freshness,
			MaxStalenessSeconds: ep.MaxStalenessSeconds,
			SelfDescribing:      ep.SelfDescribe,
		}
		if mp, ok := a.bundle.Masks.Policies[ep.MaskPolicy]; ok {
			v.Mask = &maskView{
				Default:      mp.Default,
				Fallback:     mp.Fallback,
				Fields:       mp.Fields,
				RejectFields: mp.RejectFields,
			}
		}
		resp.Endpoints = append(resp.Endpoints, v)
	}
	sort.Slice(resp.Endpoints, func(i, j int) bool {
		if resp.Endpoints[i].Path == resp.Endpoints[j].Path {
			return resp.Endpoints[i].Method < resp.Endpoints[j].Method
		}
		return resp.Endpoints[i].Path < resp.Endpoints[j].Path
	})
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleProcess(w http.ResponseWriter, r *http.Request) {
	var env Envelope
	if err := a.decode(w, r, &env); err != nil {
		a.writeError(w, err)
		return
	}
	req, err := a.engineRequest(env)
	if err != nil {
		a.writeError(w, err)
		return
	}
	res := a.eng.ProcessRequest(r.Context(), req)
	out := ProcessResult{Decision: res}
	if res.Allowed && env.Response != nil && len(env.Response.Body) > 0 {
		ep := a.eng.Endpoint(req.Method, req.Path)
		masked, err := a.eng.MaskResponseCtx(r.Context(), ep, env.Response.Body, env.Response.Status >= 400)
		if err != nil {
			a.writeError(w, &apiError{http.StatusInternalServerError, "mask_failed", err.Error()})
			return
		}
		out.Response = &Response{Status: env.Response.Status, Body: masked}
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleTest(w http.ResponseWriter, r *http.Request) {
	var f conformance.Fixture
	if err := a.decode(w, r, &f); err != nil {
		a.writeError(w, err)
		return
	}
	if strings.TrimSpace(f.Request.Method) == "" || strings.TrimSpace(f.Request.Path) == "" {
		a.writeError(w, &apiError{http.StatusBadRequest, "invalid_fixture", "request.method and request.path are required"})
		return
	}
	if f.Name == "" {
		f.Name = "inline"
	}
	ev, err := conformance.Evaluate(r.Context(), a.bundle, &f, a.pip)
	if err != nil {
		a.writeError(w, &apiError{http.StatusInternalServerError, "evaluation_failed", err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (a *API) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(contracts.TestAPISpec)
}

func (a *API) handleDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(consoleHTML)
}

// engineRequest validates the envelope and derives the engine request.
func (a *API) engineRequest(env Envelope) (engine.Request, error) {
	method := strings.ToUpper(strings.TrimSpace(env.Request.Method))
	path := strings.TrimSpace(env.Request.Path)
	if method == "" {
		return engine.Request{}, &apiError{http.StatusBadRequest, "invalid_request", "request.method is required"}
	}
	if path == "" {
		return engine.Request{}, &apiError{http.StatusBadRequest, "invalid_request", "request.path is required"}
	}
	if !strings.HasPrefix(path, "/") {
		return engine.Request{}, &apiError{http.StatusBadRequest, "invalid_request", "request.path must start with /"}
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	subject := subjectFromHeaders(env.Request.Headers)
	if env.Subject != nil {
		if env.Subject.Sub != "" {
			subject.Sub = env.Subject.Sub
		}
		if subject.Claims == nil {
			subject.Claims = map[string]string{}
		}
		for k, v := range env.Subject.Claims {
			subject.Claims[k] = v
		}
	}
	return engine.Request{
		Method:  method,
		Path:    path,
		Body:    env.Request.Body,
		Subject: subject,
	}, nil
}

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

type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.message }

func (a *API) decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, a.maxBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return &apiError{http.StatusRequestEntityTooLarge, "payload_too_large",
				fmt.Sprintf("body exceeds %d bytes", a.maxBody)}
		}
		return &apiError{http.StatusBadRequest, "invalid_json", err.Error()}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &apiError{http.StatusBadRequest, "invalid_json", "body must contain one JSON value"}
	}
	return nil
}

func (a *API) writeError(w http.ResponseWriter, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		ae = &apiError{http.StatusInternalServerError, "internal_error", err.Error()}
	}
	writeError(w, ae.status, ae.code, ae.message)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func (a *API) withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (a *API) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		a.log.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
			"remote", r.RemoteAddr,
		)
	})
}
