package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/controls/core/internal/pipclient"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	repo, err := LoadMemoryRepo("../fixtures/attributes.json")
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(repo, log).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func TestBatchProjectsFields(t *testing.T) {
	srv := newTestServer(t)
	body, _ := json.Marshal(pipclient.BatchRequest{Requests: []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId", "region"}},
		{Type: "account", ID: "missing", Fields: []string{"ownerId"}},
	}})
	resp, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out pipclient.BatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 2 {
		t.Fatalf("results = %d", len(out.Results))
	}
	if _, ok := out.Results[0].Attributes["ssn"]; ok {
		t.Fatalf("projection leaked ssn: %v", out.Results[0].Attributes)
	}
	if out.Results[0].Attributes["ownerId"] != "u-1" {
		t.Fatalf("ownerId = %v", out.Results[0].Attributes["ownerId"])
	}
	if out.Results[1].Found {
		t.Fatalf("missing resource should be found=false")
	}
}

func TestClassification(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/v1/classification/account")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["resourceType"] != "account" {
		t.Fatalf("got %v", out)
	}
}
