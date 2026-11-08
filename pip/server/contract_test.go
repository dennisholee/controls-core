package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"example.com/controls/core/internal/pipclient"
)

// TestResponsesMatchOpenAPI validates real PIP responses against the published
// contract, not just the required-field checks in the client.
func TestResponsesMatchOpenAPI(t *testing.T) {
	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromFile("../../contracts/pip-api.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The test server is on localhost; ignore the published server host.
	spec.Servers = nil
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t)

	batch, _ := json.Marshal(pipclient.BatchRequest{Requests: []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId", "region"}},
	}})
	cases := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/v1/attributes:batch", batch},
		{http.MethodGet, "/v1/classification/account", nil},
		{http.MethodGet, "/version", nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, bytes.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if tc.body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			route, params, err := router.FindRoute(req)
			if err != nil {
				t.Fatal(err)
			}
			input := &openapi3filter.RequestValidationInput{
				Request: req, PathParams: params, Route: route,
			}
			if err := openapi3filter.ValidateRequest(context.Background(), input); err != nil {
				t.Fatalf("request does not match contract: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if err := openapi3filter.ValidateResponse(context.Background(), &openapi3filter.ResponseValidationInput{
				RequestValidationInput: input,
				Status:                 resp.StatusCode,
				Header:                 resp.Header,
				Body:                   resp.Body,
			}); err != nil {
				t.Fatalf("response does not match contract: %v", err)
			}
		})
	}
}
