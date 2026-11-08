package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"example.com/controls/core/internal/metrics"
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
	if out.Results[0].Version != "fixture-v1" {
		t.Fatalf("version = %q", out.Results[0].Version)
	}
	if out.Results[1].Found {
		t.Fatalf("missing resource should be found=false")
	}
}

func TestLagSecondsReported(t *testing.T) {
	repo, err := LoadMemoryRepo("../fixtures/attributes.json")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(repo, log, WithLagSeconds(7)).Handler())
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(pipclient.BatchRequest{Requests: []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId"}},
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
	if out.Results[0].LagSeconds != 7 {
		t.Fatalf("lag = %d", out.Results[0].LagSeconds)
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

func TestBatchCapEnforced(t *testing.T) {
	srv := newTestServer(t)
	reqs := make([]pipclient.Ref, 101)
	for i := range reqs {
		reqs[i] = pipclient.Ref{Type: "account", ID: "A123"}
	}
	body, _ := json.Marshal(pipclient.BatchRequest{Requests: reqs})
	resp, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestMetricsRecorded(t *testing.T) {
	repo, err := LoadMemoryRepo("../fixtures/attributes.json")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := metrics.New()
	srv := httptest.NewServer(New(repo, log, WithMetrics(reg)).Handler())
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(pipclient.BatchRequest{Requests: []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId"}},
		{Type: "account", ID: "missing", Fields: []string{"ownerId"}},
	}})
	resp, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	var out bytes.Buffer
	reg.Write(&out)
	text := out.String()
	for _, want := range []string{
		`pip_requests_total{endpoint="attributes"} 1`,
		`pip_attributes_total{found="true",resource_type="account"} 1`,
		`pip_attributes_total{found="false",resource_type="account"} 1`,
		`pip_request_duration_seconds_count{endpoint="attributes"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

type blockingRepo struct {
	repo    Repository
	entered chan struct{}
	release chan struct{}
}

func (b blockingRepo) Get(ctx context.Context, typ, id string, fields []string) (Resource, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return b.repo.Get(ctx, typ, id, fields)
}

func (b blockingRepo) Classification(ctx context.Context, typ string) (map[string]string, bool, error) {
	return b.repo.Classification(ctx, typ)
}

func TestConcurrencyLimit(t *testing.T) {
	base, err := LoadMemoryRepo("../fixtures/attributes.json")
	if err != nil {
		t.Fatal(err)
	}
	repo := blockingRepo{repo: base, entered: make(chan struct{}, 1), release: make(chan struct{})}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(repo, log, WithMaxConcurrent(1)).Handler())
	t.Cleanup(srv.Close)

	body, _ := json.Marshal(pipclient.BatchRequest{Requests: []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId"}},
	}})
	go func() {
		resp, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-repo.entered

	resp, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	close(repo.release)
}

type forbiddenRepo struct{}

func (forbiddenRepo) Get(context.Context, string, string, []string) (Resource, error) {
	return Resource{}, ErrForbidden
}
func (forbiddenRepo) Classification(context.Context, string) (map[string]string, bool, error) {
	return nil, false, nil
}

func TestForbiddenCallerMapsTo403(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(forbiddenRepo{}, log).Handler())
	t.Cleanup(srv.Close)
	body, _ := json.Marshal(pipclient.BatchRequest{Requests: []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId"}},
	}})
	resp, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestRequireCaller(t *testing.T) {
	repo, err := LoadMemoryRepo("../fixtures/attributes.json")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(repo, log, WithRequireCaller(true)).Handler())
	t.Cleanup(srv.Close)
	body, _ := json.Marshal(pipclient.BatchRequest{Requests: []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId"}},
	}})
	resp, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestRateLimit(t *testing.T) {
	repo, err := LoadMemoryRepo("../fixtures/attributes.json")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(repo, log, WithRateLimit(1, 1)).Handler())
	t.Cleanup(srv.Close)
	body, _ := json.Marshal(pipclient.BatchRequest{Requests: []pipclient.Ref{
		{Type: "account", ID: "A123", Fields: []string{"ownerId"}},
	}})
	first, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d", first.StatusCode)
	}
	second, err := http.Post(srv.URL+"/v1/attributes:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second status = %d", second.StatusCode)
	}
}

func TestVersionEndpoint(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/version")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		APIVersion string `json:"apiVersion"`
		Build      string `json:"build"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.APIVersion != APIVersion || out.Build == "" {
		t.Fatalf("version = %+v", out)
	}
}

func TestServerMatchesPublishedContract(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/pip-api.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Info struct {
			Version string `yaml:"version"`
		} `yaml:"info"`
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Info.Version != APIVersion {
		t.Fatalf("contract version %q != server %q", spec.Info.Version, APIVersion)
	}
	if _, ok := spec.Paths["/version"]; !ok {
		t.Fatal("contract is missing /version")
	}
}
