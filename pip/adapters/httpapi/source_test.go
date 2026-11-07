package httpapi

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/controls/core/pip/server"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestGetProjectsAllowlistAndLag(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/accounts/A123" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("X-Replica-Lag-Seconds", "12")
		w.Header().Set("X-Resource-Version", `"v42"`)
		_, _ = w.Write([]byte(`{"ownerId":"u-1","region":"US","ssn":"111","extra":"x"}`))
	}))
	defer upstream.Close()

	t.Setenv("ATTR_TOKEN", "test-token")
	cfg := writeConfig(t, `{"sources":{"account":{
	  "url":"`+upstream.URL+`/accounts/{id}",
	  "headers":{"Authorization":"Bearer ${ATTR_TOKEN}"},
	  "fields":["ownerId","region"],
	  "classification":{"ssn":"pii.locked"}
	}}}`)
	repo, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := repo.Get(context.Background(), "account", "A123", []string{"ownerId", "region", "ssn"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Found || res.LagSeconds != 12 {
		t.Fatalf("resource = %+v", res)
	}
	if res.Version != "v42" {
		t.Fatalf("version = %q", res.Version)
	}
	if _, ok := res.Attributes["ssn"]; ok {
		t.Fatalf("projection leaked ssn: %v", res.Attributes)
	}
	if res.Attributes["ownerId"] != "u-1" || res.Attributes["region"] != "US" {
		t.Fatalf("attrs = %v", res.Attributes)
	}
}

func TestNotFoundAndUpstreamError(t *testing.T) {
	status := http.StatusNotFound
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer upstream.Close()
	cfg := writeConfig(t, `{"sources":{"account":{"url":"`+upstream.URL+`/{id}","fields":["ownerId"]}}}`)
	repo, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := repo.Get(context.Background(), "account", "A1", nil)
	if err != nil || res.Found {
		t.Fatalf("not found: %+v %v", res, err)
	}
	status = http.StatusInternalServerError
	if _, err := repo.Get(context.Background(), "account", "A1", nil); err == nil {
		t.Fatal("expected upstream error")
	}
}

func TestClassification(t *testing.T) {
	cfg := writeConfig(t, `{"sources":{"account":{"url":"http://127.0.0.1/{id}","fields":["ownerId"],"classification":{"ssn":"pii.locked"}}}}`)
	repo, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	classes, ok, err := repo.Classification(context.Background(), "account")
	if err != nil || !ok || classes["ssn"] != "pii.locked" {
		t.Fatalf("classification = %v %v %v", classes, ok, err)
	}
}

func TestConfigValidation(t *testing.T) {
	cases := []string{
		`{"sources":{"account":{"url":"http://127.0.0.1/accounts","fields":["ownerId"]}}}`,
		`{"sources":{"account":{"url":"http://127.0.0.1/{id}","fields":[]}}}`,
		`{"sources":{"account":{"url":"ftp://127.0.0.1/{id}","fields":["x"]}}}`,
	}
	for _, body := range cases {
		if _, err := Load(writeConfig(t, body)); err == nil {
			t.Fatalf("expected validation error for %s", body)
		}
	}
}

func TestTLSWithCustomCA(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ownerId":"u-1"}`))
	}))
	defer upstream.Close()

	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: upstream.Certificate().Raw})
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, ca, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := writeConfig(t, `{"sources":{"account":{
	  "url":"`+upstream.URL+`/accounts/{id}",
	  "caFile":"`+caPath+`",
	  "fields":["ownerId"]
	}}}`)
	repo, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := repo.Get(context.Background(), "account", "A123", []string{"ownerId"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Found || res.Attributes["ownerId"] != "u-1" {
		t.Fatalf("resource = %+v", res)
	}
}

func TestResponseSizeLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ownerId":"` + strings.Repeat("x", 1<<20) + `"}`))
	}))
	defer upstream.Close()
	cfg := writeConfig(t, `{"sources":{"account":{
	  "url":"`+upstream.URL+`/accounts/{id}",
	  "fields":["ownerId"]
	}}}`)
	repo, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(context.Background(), "account", "A123", nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestRejectsIDInHost(t *testing.T) {
	cfg := writeConfig(t, `{"sources":{"account":{
	  "url":"http://{id}.example.com/accounts",
	  "fields":["ownerId"]
	}}}`)
	if _, err := Load(cfg); err == nil {
		t.Fatal("expected host interpolation error")
	}
}

func TestCallerAuthorization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ownerId":"u-1","ssn":"111"}`))
	}))
	defer upstream.Close()
	cfg := writeConfig(t, `{"sources":{"account":{
	  "url":"`+upstream.URL+`/{id}",
	  "fields":["ownerId","ssn"],
	  "allowedCallers":["spiffe://example/app/sa/reader","spiffe://example/app/sa/pii-reader"],
	  "fieldCallers":{"ssn":["spiffe://example/app/sa/pii-reader"]}
	}}}`)
	repo, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reader := server.WithCaller(context.Background(), "spiffe://example/app/sa/reader")
	if _, err := repo.Get(reader, "account", "A1", []string{"ownerId"}); err != nil {
		t.Fatalf("allowed caller: %v", err)
	}
	if _, err := repo.Get(reader, "account", "A1", []string{"ssn"}); !errors.Is(err, server.ErrForbidden) {
		t.Fatalf("expected field forgiveness, got %v", err)
	}
	pii := server.WithCaller(context.Background(), "spiffe://example/app/sa/pii-reader")
	if _, err := repo.Get(pii, "account", "A1", []string{"ssn"}); err != nil {
		t.Fatalf("pii reader: %v", err)
	}
	other := server.WithCaller(context.Background(), "spiffe://example/app/sa/other")
	if _, err := repo.Get(other, "account", "A1", []string{"ownerId"}); !errors.Is(err, server.ErrForbidden) {
		t.Fatalf("expected caller forgiveness, got %v", err)
	}
	if _, err := repo.Get(context.Background(), "account", "A1", []string{"ownerId"}); !errors.Is(err, server.ErrForbidden) {
		t.Fatalf("missing caller should be forbidden, got %v", err)
	}
}

func TestOmittedFieldsCannotBypassFieldAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ownerId":"u-1","ssn":"111"}`))
	}))
	defer upstream.Close()
	cfg := writeConfig(t, `{"sources":{"account":{
	  "url":"`+upstream.URL+`/{id}",
	  "fields":["ownerId","ssn"],
	  "allowedCallers":["spiffe://example/app/sa/reader"],
	  "fieldCallers":{"ssn":["spiffe://example/app/sa/pii-reader"]}
	}}}`)
	repo, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	reader := server.WithCaller(context.Background(), "spiffe://example/app/sa/reader")
	if _, err := repo.Get(reader, "account", "A1", nil); !errors.Is(err, server.ErrForbidden) {
		t.Fatalf("omitted fields should be forbidden, got %v", err)
	}
}
