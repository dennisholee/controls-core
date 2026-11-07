package openapidiff

import (
	"encoding/json"
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
