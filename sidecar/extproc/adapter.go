// Package extproc adapts the controls engine to Envoy's External Processing
// filter (ext_proc).
//
// Envoy sends a bidirectional stream for each HTTP request: request headers,
// request body, response headers, response body. With BUFFERED body modes each
// body arrives in a single message, but this adapter tolerates chunking.
//
// The adapter owns transport concerns only: header extraction, buffering,
// immediate responses and body mutation. Decisions and masking remain in
// internal/engine.
package extproc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
)

// DefaultMaxBody bounds buffering per request and response.
const DefaultMaxBody = 1 << 20 // 1 MiB

// Config configures the adapter.
type Config struct {
	Engine *engine.Engine
	// EngineProvider, when set, is consulted per message so a reload can swap
	// the engine without restarting the stream adapter.
	EngineProvider func() *engine.Engine
	Log            *slog.Logger
	MaxBodyBytes   int
	// IdentityKey, when set, requires a gateway-signed x-controls-identity
	// envelope and ignores raw x-subject/x-claim-* headers.
	IdentityKey     []byte
	IdentityMaxSkew time.Duration
}

// Adapter implements the ext_proc ExternalProcessor service.
type Adapter struct {
	extprocv3.UnimplementedExternalProcessorServer
	engineFn        func() *engine.Engine
	log             *slog.Logger
	max             int
	identityKey     []byte
	identityMaxSkew time.Duration
}

// New builds an adapter.
func New(cfg Config) *Adapter {
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBody
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	fn := cfg.EngineProvider
	if fn == nil {
		fixed := cfg.Engine
		fn = func() *engine.Engine { return fixed }
	}
	skew := cfg.IdentityMaxSkew
	if len(cfg.IdentityKey) > 0 && skew <= 0 {
		skew = time.Minute
	}
	return &Adapter{engineFn: fn, log: cfg.Log, max: cfg.MaxBodyBytes, identityKey: cfg.IdentityKey, identityMaxSkew: skew}
}

// stream is the subset of the gRPC stream the adapter needs (mockable).
type stream interface {
	Context() context.Context
	Recv() (*extprocv3.ProcessingRequest, error)
	Send(*extprocv3.ProcessingResponse) error
}

// Process implements the gRPC method.
func (a *Adapter) Process(s extprocv3.ExternalProcessor_ProcessServer) error {
	return a.Handle(s)
}

// Handle runs the per-request message loop.
func (a *Adapter) Handle(s stream) error {
	// Pin one engine for the whole stream so a reload cannot mix bundles
	// between the decision and the response-masking phase.
	st := &state{headers: map[string]string{}, eng: a.engineFn()}
	for {
		msg, err := s.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		resp, done, err := a.handleMessage(s.Context(), st, msg)
		if err != nil {
			return err
		}
		if resp != nil {
			if err := s.Send(resp); err != nil {
				return err
			}
		}
		if done {
			return nil
		}
	}
}

type state struct {
	method   string
	path     string
	headers  map[string]string
	reqBody  []byte
	status   int
	respBody []byte
	endpoint *config.Endpoint
	eng      *engine.Engine
	decided  bool
}

func (a *Adapter) handleMessage(ctx context.Context, st *state, msg *extprocv3.ProcessingRequest) (*extprocv3.ProcessingResponse, bool, error) {
	switch req := msg.GetRequest().(type) {
	case *extprocv3.ProcessingRequest_RequestHeaders:
		if a.log.Enabled(context.Background(), slog.LevelDebug) {
			keys := make([]string, 0, len(req.RequestHeaders.GetHeaders().GetHeaders()))
			for _, h := range req.RequestHeaders.GetHeaders().GetHeaders() {
				keys = append(keys, h.GetKey())
			}
			a.log.Debug("request headers", "keys", keys, "attributes", len(req.RequestHeaders.GetAttributes()))
		}
		for _, h := range req.RequestHeaders.GetHeaders().GetHeaders() {
			st.headers[strings.ToLower(h.GetKey())] = headerValue(h)
		}
		st.method = st.headers[":method"]
		st.path = st.headers[":path"]
		if req.RequestHeaders.GetEndOfStream() {
			immediate, err := a.decide(ctx, st)
			if err != nil || immediate != nil {
				return immediate, immediate != nil, err
			}
		}
		return continueRequestHeaders(), false, nil

	case *extprocv3.ProcessingRequest_RequestBody:
		st.reqBody = append(st.reqBody, req.RequestBody.GetBody()...)
		if len(st.reqBody) > a.max {
			return a.immediate(typev3.StatusCode_PayloadTooLarge, "request_body_too_large"), true, nil
		}
		if !req.RequestBody.GetEndOfStream() {
			return continueRequestBody(), false, nil
		}
		immediate, err := a.decide(ctx, st)
		if err != nil || immediate != nil {
			return immediate, immediate != nil, err
		}
		return continueRequestBody(), false, nil

	case *extprocv3.ProcessingRequest_RequestTrailers:
		return continueRequestTrailers(), false, nil

	case *extprocv3.ProcessingRequest_ResponseHeaders:
		for _, h := range req.ResponseHeaders.GetHeaders().GetHeaders() {
			if strings.EqualFold(h.GetKey(), ":status") {
				switch strings.TrimSpace(headerValue(h)) {
				case "200":
					st.status = 200
				case "400":
					st.status = 400
				case "401":
					st.status = 401
				case "403":
					st.status = 403
				case "404":
					st.status = 404
				case "500":
					st.status = 500
				case "503":
					st.status = 503
				}
			}
		}
		return continueResponseHeaders(), false, nil

	case *extprocv3.ProcessingRequest_ResponseBody:
		st.respBody = append(st.respBody, req.ResponseBody.GetBody()...)
		if !req.ResponseBody.GetEndOfStream() {
			return continueResponseBody(), false, nil
		}
		if st.endpoint == nil {
			return continueResponseBody(), false, nil
		}
		if len(st.respBody) > a.max {
			return a.immediate(typev3.StatusCode_InternalServerError, "response_too_large_to_mask"), true, nil
		}
		masked, err := st.eng.MaskResponseCtx(ctx, st.endpoint, st.respBody, st.status >= 400)
		if err != nil {
			return nil, false, err
		}
		if string(masked) == string(st.respBody) {
			return continueResponseBody(), false, nil
		}
		return mutateResponseBody(masked), false, nil

	case *extprocv3.ProcessingRequest_ResponseTrailers:
		return continueResponseTrailers(), false, nil
	}
	return nil, false, nil
}

// decide evaluates the request and returns an immediate response on denial.
// A nil response means "continue"; the caller picks the correct continue type
// for the message it is answering.
func (a *Adapter) decide(ctx context.Context, st *state) (*extprocv3.ProcessingResponse, error) {
	if st.decided {
		return nil, nil
	}
	st.decided = true
	subject, err := a.subject(st.headers)
	if err != nil {
		a.log.Warn("rejected untrusted identity", "err", err)
		return a.immediateWithReason(typev3.StatusCode_Forbidden, "identity_untrusted"), nil
	}
	res := st.eng.ProcessRequest(ctx, engine.Request{
		Method:  st.method,
		Path:    st.path,
		Body:    st.reqBody,
		Subject: subject,
	})
	st.endpoint = st.eng.Endpoint(st.method, st.path)
	a.log.Debug("decision",
		"endpoint", res.EndpointID, "method", st.method, "path", st.path,
		"allowed", res.Allowed, "status", res.Status, "reason", res.Reason,
		"violations", len(res.Violations))
	if res.Allowed {
		return nil, nil
	}
	return a.immediateWithReason(statusCode(res.Status), res.Reason), nil
}

// headerValue reads the string form, falling back to raw bytes. Envoy sends
// pseudo-headers (:method, :path, :status) as raw_value only.
func headerValue(h *corev3.HeaderValue) string {
	if v := h.GetValue(); v != "" {
		return v
	}
	return string(h.GetRawValue())
}

func (a *Adapter) subject(headers map[string]string) (engine.Subject, error) {
	if len(a.identityKey) > 0 {
		return verifyIdentity(headers[identityHeader], a.identityKey, time.Now(), a.identityMaxSkew)
	}
	return subjectFromHeaders(headers), nil
}

func subjectFromHeaders(headers map[string]string) engine.Subject {
	sub := engine.Subject{
		Sub:    headers["x-subject"],
		Claims: map[string]string{},
	}
	for k, v := range headers {
		if strings.HasPrefix(k, "x-claim-") {
			sub.Claims[strings.TrimPrefix(k, "x-claim-")] = v
		}
	}
	return sub
}

func (a *Adapter) immediate(code typev3.StatusCode, reason string) *extprocv3.ProcessingResponse {
	return a.immediateWithReason(code, reason)
}

func (a *Adapter) immediateWithReason(code typev3.StatusCode, reason string) *extprocv3.ProcessingResponse {
	body, _ := json.Marshal(map[string]string{"error": reason})
	return &extprocv3.ProcessingResponse{
		Response: &extprocv3.ProcessingResponse_ImmediateResponse{
			ImmediateResponse: &extprocv3.ImmediateResponse{
				Status: &typev3.HttpStatus{Code: code},
				Body:   body,
				Headers: &extprocv3.HeaderMutation{
					SetHeaders: []*corev3.HeaderValueOption{{
						Header: &corev3.HeaderValue{Key: "content-type", Value: "application/json"},
					}},
				},
			},
		},
	}
}

func statusCode(status int) typev3.StatusCode {
	switch status {
	case 400:
		return typev3.StatusCode_BadRequest
	case 401:
		return typev3.StatusCode_Unauthorized
	case 404:
		return typev3.StatusCode_NotFound
	case 503:
		return typev3.StatusCode_ServiceUnavailable
	default:
		return typev3.StatusCode_Forbidden
	}
}

func continueRequestHeaders() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestHeaders{
		RequestHeaders: &extprocv3.HeadersResponse{},
	}}
}

func continueRequestBody() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestBody{
		RequestBody: &extprocv3.BodyResponse{Response: &extprocv3.CommonResponse{}},
	}}
}

func continueRequestTrailers() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_RequestTrailers{
		RequestTrailers: &extprocv3.TrailersResponse{},
	}}
}

func continueResponseHeaders() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseHeaders{
		ResponseHeaders: &extprocv3.HeadersResponse{},
	}}
}

func continueResponseBody() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseBody{
		ResponseBody: &extprocv3.BodyResponse{Response: &extprocv3.CommonResponse{}},
	}}
}

func continueResponseTrailers() *extprocv3.ProcessingResponse {
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseTrailers{
		ResponseTrailers: &extprocv3.TrailersResponse{},
	}}
}

func mutateResponseBody(body []byte) *extprocv3.ProcessingResponse {
	// Envoy rejects a mutated body whose length differs from Content-Length
	// unless the header is updated in the same response.
	return &extprocv3.ProcessingResponse{Response: &extprocv3.ProcessingResponse_ResponseBody{
		ResponseBody: &extprocv3.BodyResponse{Response: &extprocv3.CommonResponse{
			HeaderMutation: &extprocv3.HeaderMutation{
				SetHeaders: []*corev3.HeaderValueOption{{
					Header: &corev3.HeaderValue{
						Key:      "content-length",
						RawValue: []byte(strconv.Itoa(len(body))),
					},
				}},
			},
			BodyMutation: &extprocv3.BodyMutation{
				Mutation: &extprocv3.BodyMutation_Body{Body: body},
			},
		}},
	}}
}
