package extproc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/pipclient"
)

type fakeStream struct {
	reqs []*extprocv3.ProcessingRequest
	sent []*extprocv3.ProcessingResponse
	i    int
}

func (f *fakeStream) Context() context.Context { return context.Background() }
func (f *fakeStream) Send(r *extprocv3.ProcessingResponse) error {
	f.sent = append(f.sent, r)
	return nil
}
func (f *fakeStream) Recv() (*extprocv3.ProcessingRequest, error) {
	if f.i >= len(f.reqs) {
		return nil, io.EOF
	}
	r := f.reqs[f.i]
	f.i++
	return r, nil
}

type fakePIP struct{ err error }

func (f fakePIP) Resolve(_ context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]pipclient.Attribute{}
	for _, r := range refs {
		if r.Type == "account" && r.ID == "A123" {
			out["account|A123"] = pipclient.Attribute{
				Type: "account", ID: "A123", Found: true,
				Attributes: map[string]any{"ownerId": "u-1", "region": "US", "status": "active"},
			}
		}
	}
	return out, nil
}

func (f fakePIP) Classification(context.Context, string) (map[string]string, error) {
	return nil, f.err
}

func (f fakePIP) ResolveFresh(ctx context.Context, refs []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	return f.Resolve(ctx, refs)
}

func (f fakePIP) CachedClassification(string) (map[string]string, bool) { return nil, false }

func newAdapter(t *testing.T, pip engine.PIP) *Adapter {
	t.Helper()
	bundle, err := config.LoadDir("../../integration/testdata/config")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Config{Processor: engine.New(bundle, pip), Log: log})
}

func headerReq(method, path string, extra map[string]string) *extprocv3.ProcessingRequest {
	hs := []*corev3.HeaderValue{
		{Key: ":method", Value: method},
		{Key: ":path", Value: path},
	}
	for k, v := range extra {
		hs = append(hs, &corev3.HeaderValue{Key: k, Value: v})
	}
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestHeaders{
		RequestHeaders: &extprocv3.HttpHeaders{Headers: &corev3.HeaderMap{Headers: hs}},
	}}
}

func bodyReq(body string) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_RequestBody{
		RequestBody: &extprocv3.HttpBody{Body: []byte(body), EndOfStream: true},
	}}
}

func respHeadersReq(status string) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_ResponseHeaders{
		ResponseHeaders: &extprocv3.HttpHeaders{Headers: &corev3.HeaderMap{Headers: []*corev3.HeaderValue{
			{Key: ":status", Value: status},
		}}},
	}}
}

func respBodyReq(body string) *extprocv3.ProcessingRequest {
	return &extprocv3.ProcessingRequest{Request: &extprocv3.ProcessingRequest_ResponseBody{
		ResponseBody: &extprocv3.HttpBody{Body: []byte(body), EndOfStream: true},
	}}
}

func TestAllowedRequestMasksResponse(t *testing.T) {
	a := newAdapter(t, fakePIP{})
	s := &fakeStream{reqs: []*extprocv3.ProcessingRequest{
		headerReq("GET", "/accounts/A123", map[string]string{"x-subject": "u-1", "x-claim-region": "US"}),
		bodyReq(""),
		respHeadersReq("200"),
		respBodyReq(`{"accountNumber":"1234567890","ssn":"111-22-3333","region":"US"}`),
	}}
	if err := a.Handle(s); err != nil {
		t.Fatal(err)
	}
	if len(s.sent) != 4 {
		t.Fatalf("responses = %d", len(s.sent))
	}
	last := s.sent[len(s.sent)-1]
	body := last.GetResponseBody().GetResponse().GetBodyMutation().GetBody()
	if len(body) == 0 {
		t.Fatalf("expected body mutation, got %+v", last)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["ssn"] != "[REDACTED]" || doc["accountNumber"] != "******7890" {
		t.Fatalf("unexpected mask: %v", doc)
	}
	headers := last.GetResponseBody().GetResponse().GetHeaderMutation().GetSetHeaders()
	if len(headers) != 1 || string(headers[0].GetHeader().GetRawValue()) != strconv.Itoa(len(body)) {
		t.Fatalf("content-length not updated: %+v", headers)
	}
}

func TestDeniedRequestReturnsImmediate403(t *testing.T) {
	a := newAdapter(t, fakePIP{})
	s := &fakeStream{reqs: []*extprocv3.ProcessingRequest{
		headerReq("GET", "/accounts/A123", map[string]string{"x-subject": "u-2", "x-claim-region": "EU"}),
		bodyReq(""),
		respHeadersReq("200"),
	}}
	if err := a.Handle(s); err != nil {
		t.Fatal(err)
	}
	last := s.sent[len(s.sent)-1]
	im := last.GetImmediateResponse()
	if im == nil {
		t.Fatalf("expected immediate response, got %+v", last)
	}
	if im.GetStatus().GetCode().String() != "Forbidden" {
		t.Fatalf("status = %v", im.GetStatus().GetCode())
	}
}

func TestPIPFailureReturns503(t *testing.T) {
	a := newAdapter(t, fakePIP{err: errors.New("down")})
	s := &fakeStream{reqs: []*extprocv3.ProcessingRequest{
		headerReq("GET", "/accounts/A123", map[string]string{"x-subject": "u-1"}),
		bodyReq(""),
	}}
	if err := a.Handle(s); err != nil {
		t.Fatal(err)
	}
	last := s.sent[len(s.sent)-1]
	im := last.GetImmediateResponse()
	if im == nil || im.GetStatus().GetCode().String() != "ServiceUnavailable" {
		t.Fatalf("expected 503, got %+v", last)
	}
}

func TestSelfDescribingMismatchReturns403(t *testing.T) {
	a := newAdapter(t, fakePIP{})
	s := &fakeStream{reqs: []*extprocv3.ProcessingRequest{
		headerReq("POST", "/payments", map[string]string{"x-subject": "u-1"}),
		bodyReq(`{"_type":"account","_id":"P-9001","amount":10,"ownerId":"u-1"}`),
	}}
	if err := a.Handle(s); err != nil {
		t.Fatal(err)
	}
	im := s.sent[len(s.sent)-1].GetImmediateResponse()
	if im == nil {
		t.Fatalf("expected immediate response")
	}
}

// Envoy sends pseudo-headers as raw_value only. A bodyless request (GET) also
// sets end_of_stream on the headers message, so the decision must happen there.
func TestBodylessRequestWithRawPseudoHeaders(t *testing.T) {
	a := newAdapter(t, fakePIP{})
	hs := []*corev3.HeaderValue{
		{Key: ":method", RawValue: []byte("GET")},
		{Key: ":path", RawValue: []byte("/accounts/A123")},
		{Key: "x-subject", Value: "u-1"},
		{Key: "x-claim-region", Value: "US"},
	}
	s := &fakeStream{reqs: []*extprocv3.ProcessingRequest{
		{Request: &extprocv3.ProcessingRequest_RequestHeaders{
			RequestHeaders: &extprocv3.HttpHeaders{
				Headers:     &corev3.HeaderMap{Headers: hs},
				EndOfStream: true,
			},
		}},
		respHeadersReq("200"),
		respBodyReq(`{"ssn":"111-22-3333","accountNumber":"1234567890"}`),
	}}
	if err := a.Handle(s); err != nil {
		t.Fatal(err)
	}
	if len(s.sent) != 3 {
		t.Fatalf("responses = %d", len(s.sent))
	}
	body := s.sent[2].GetResponseBody().GetResponse().GetBodyMutation().GetBody()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["ssn"] != "[REDACTED]" {
		t.Fatalf("not masked: %v", doc)
	}
}

// A reload swaps the engine under the adapter without restarting the stream
// handler: the provider is consulted for each request.
func TestEngineProviderSwap(t *testing.T) {
	bundle, err := config.LoadDir("../../integration/testdata/config")
	if err != nil {
		t.Fatal(err)
	}
	allow := engine.New(bundle, fakePIP{})
	denyBundle := *bundle
	denyBundle.Rules = config.Rules{Version: "1.0", DefaultEffect: "deny"}
	deny := engine.New(&denyBundle, fakePIP{})

	current := allow
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := New(Config{ProcessorProvider: func() Processor { return current }, Log: log})

	run := func() *extprocv3.ProcessingResponse {
		s := &fakeStream{reqs: []*extprocv3.ProcessingRequest{
			headerReq("GET", "/accounts/A123", map[string]string{"x-subject": "u-1", "x-claim-region": "US"}),
			bodyReq(""),
			respHeadersReq("200"),
			respBodyReq(`{"ssn":"111-22-3333"}`),
		}}
		if err := a.Handle(s); err != nil {
			t.Fatal(err)
		}
		return s.sent[len(s.sent)-1]
	}

	if first := run(); first.GetImmediateResponse() != nil {
		t.Fatalf("expected allow before reload, got %+v", first)
	}
	current = deny
	if second := run(); second.GetImmediateResponse() == nil {
		t.Fatalf("expected deny after reload, got %+v", second)
	}
}

// The adapter depends on the Processor port, not on the concrete engine.
type fakeProcessor struct {
	requests int
	masks    int
}

func (f *fakeProcessor) StartupError() error { return nil }
func (f *fakeProcessor) ProcessRequest(context.Context, engine.Request) engine.Result {
	f.requests++
	return engine.Result{Allowed: true, Status: 200, EndpointID: "stub"}
}
func (f *fakeProcessor) MaskResponseCtx(_ context.Context, _ *config.Endpoint, body []byte, _ bool) ([]byte, error) {
	f.masks++
	return body, nil
}
func (f *fakeProcessor) Endpoint(string, string) *config.Endpoint {
	return &config.Endpoint{ID: "stub"}
}

func TestAdapterUsesProcessorPort(t *testing.T) {
	proc := &fakeProcessor{}
	a := New(Config{Processor: proc, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	s := &fakeStream{reqs: []*extprocv3.ProcessingRequest{
		headerReq("GET", "/stub", map[string]string{"x-subject": "u-1"}),
		bodyReq(""),
		respHeadersReq("200"),
		respBodyReq(`{"ok":true}`),
	}}
	if err := a.Handle(s); err != nil {
		t.Fatal(err)
	}
	if proc.requests != 1 || proc.masks != 1 {
		t.Fatalf("processor calls: requests=%d masks=%d", proc.requests, proc.masks)
	}
	if im := s.sent[len(s.sent)-1].GetImmediateResponse(); im != nil {
		t.Fatalf("unexpected immediate response: %+v", im)
	}
}
