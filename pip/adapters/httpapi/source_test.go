package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
