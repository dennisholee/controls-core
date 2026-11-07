package openapidiff

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func doc(paths map[string][]string) Document {
	d := Document{Paths: map[string]map[string]json.RawMessage{}}
	for path, methods := range paths {
		d.Paths[path] = map[string]json.RawMessage{}
		for _, m := range methods {
			d.Paths[path][m] = json.RawMessage(`{}`)
		}
	}
	return d
}

func TestCheckClean(t *testing.T) {
	d := doc(map[string][]string{
		"/accounts/{accountId}": {"get"},
		"/payments":             {"post"},
	})
	res := Check(d, []Endpoint{
		{ID: "get-account", Method: "GET", Path: "/accounts/{accountId}"},
		{ID: "create-payment", Method: "POST", Path: "/payments"},
	})
	if len(res.MissingRegistry) != 0 || len(res.OrphanRegistry) != 0 {
		t.Fatalf("unexpected findings: %+v", res)
	}
}

func TestParameterNamesDoNotMatter(t *testing.T) {
	d := doc(map[string][]string{"/accounts/{id}": {"get"}})
	res := Check(d, []Endpoint{{ID: "get-account", Method: "GET", Path: "/accounts/{accountId}"}})
	if len(res.MissingRegistry) != 0 || len(res.OrphanRegistry) != 0 {
		t.Fatalf("parameter names should not create findings: %+v", res)
	}
}

func TestMissingAndOrphan(t *testing.T) {
	d := doc(map[string][]string{
		"/accounts/{accountId}": {"get"},
		"/admin/secrets":        {"get"},
	})
	res := Check(d, []Endpoint{
		{ID: "get-account", Method: "GET", Path: "/accounts/{accountId}"},
		{ID: "old-route", Method: "DELETE", Path: "/legacy"},
	})
	if len(res.MissingRegistry) != 1 || res.MissingRegistry[0] != "GET /admin/secrets" {
		t.Fatalf("missing = %v", res.MissingRegistry)
	}
	if len(res.OrphanRegistry) != 1 {
		t.Fatalf("orphan = %v", res.OrphanRegistry)
	}
}

func TestIgnoresNonOperationKeys(t *testing.T) {
	d := doc(map[string][]string{"/accounts/{accountId}": {"get", "parameters"}})
	res := Check(d, []Endpoint{{ID: "get-account", Method: "GET", Path: "/accounts/{accountId}"}})
	if len(res.MissingRegistry) != 0 {
		t.Fatalf("parameters treated as operation: %v", res.MissingRegistry)
	}
}

func TestAnnotationMismatch(t *testing.T) {
	d := Document{Paths: map[string]map[string]json.RawMessage{
		"/accounts/{accountId}": {
			"get": json.RawMessage(`{
			  "x-resource-type":"account",
			  "x-resource-id":"accountId",
			  "x-mask-policy":"account.read",
			  "x-resource-source":"pip"
			}`),
		},
	}}
	res := Check(d, []Endpoint{{
		ID: "get-account", Method: "GET", Path: "/accounts/{accountId}",
		ResourceType: "account", ResourceIDFrom: "path", ResourceID: "accountId",
		MaskPolicy: "wrong.policy", ResourceSource: "pip",
	}})
	if len(res.Mismatches) != 1 || !strings.Contains(res.Mismatches[0], "x-mask-policy") {
		t.Fatalf("mismatches = %v", res.Mismatches)
	}
}

func TestAnnotationsMatch(t *testing.T) {
	d := Document{Paths: map[string]map[string]json.RawMessage{
		"/accounts/{accountId}": {
			"get": json.RawMessage(`{
			  "x-resource-type":"account",
			  "x-resource-id":"accountId",
			  "x-mask-policy":"account.read",
			  "x-resource-source":"pip"
			}`),
		},
	}}
	res := Check(d, []Endpoint{{
		ID: "get-account", Method: "GET", Path: "/accounts/{accountId}",
		ResourceType: "account", ResourceIDFrom: "path", ResourceID: "accountId",
		MaskPolicy: "account.read", ResourceSource: "pip",
	}})
	if len(res.Mismatches) != 0 {
		t.Fatalf("unexpected mismatches: %v", res.Mismatches)
	}
}

func TestLoadYAML(t *testing.T) {
	path := t.TempDir() + "/openapi.yaml"
	body := `openapi: 3.0.3
info:
  title: test
  version: 1.0.0
paths:
  /accounts/{id}:
    get:
      x-resource-type: account
      responses:
        '200':
          description: ok
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.Paths["/accounts/{id}"]["get"]; !ok {
		t.Fatalf("paths = %+v", doc.Paths)
	}
}
