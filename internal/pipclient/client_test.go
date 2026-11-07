package pipclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveCacheState(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var req BatchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		results := make([]Attribute, 0, len(req.Requests))
		for _, ref := range req.Requests {
			results = append(results, Attribute{
				Type: ref.Type, ID: ref.ID, Found: true,
				Attributes: map[string]any{"ownerId": "u-1"},
				TTLSeconds: 60,
			})
		}
		_ = json.NewEncoder(w).Encode(BatchResponse{Results: results})
	}))
	defer srv.Close()

	c := New(srv.URL, WithDefaultTTL(time.Minute))
	refs := []Ref{{Type: "account", ID: "A123", Fields: []string{"ownerId"}}}
	first, err := c.Resolve(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	if first["account|A123"].Cache != "miss" {
		t.Fatalf("first cache = %q", first["account|A123"].Cache)
	}
	second, err := c.Resolve(context.Background(), refs)
	if err != nil {
		t.Fatal(err)
	}
	if second["account|A123"].Cache != "hit" {
		t.Fatalf("second cache = %q", second["account|A123"].Cache)
	}
	if calls != 1 {
		t.Fatalf("pip calls = %d", calls)
	}
}

func TestClassificationCached(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(ClassificationResponse{
			ResourceType: "account",
			Fields:       map[string]string{"ssn": "pii.locked"},
			TTLSeconds:   60,
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	if _, err := c.Classification(context.Background(), "account"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Classification(context.Background(), "account"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("classification calls = %d", calls)
	}
	if _, ok := c.CachedClassification("account"); !ok {
		t.Fatal("classification should be cached")
	}
}
