package extproc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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

func newAdapter(t *testing.T, pip engine.PIP) *Adapter {
	t.Helper()
	bundle, err := config.LoadDir("../../integration/testdata/config")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Config{Engine: engine.New(bundle, pip), Log: log})
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
