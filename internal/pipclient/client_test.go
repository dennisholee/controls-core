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

func TestBreakerRecoversAfterCooldown(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 5 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var req BatchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		results := make([]Attribute, 0, len(req.Requests))
		for _, ref := range req.Requests {
			results = append(results, Attribute{Type: ref.Type, ID: ref.ID, Found: true, Attributes: map[string]any{"ownerId": "u-1"}, TTLSeconds: 60})
		}
		_ = json.NewEncoder(w).Encode(BatchResponse{Results: results})
	}))
	defer srv.Close()

	c := New(srv.URL, WithBreakerCooldown(20*time.Millisecond))
	ref := []Ref{{Type: "account", ID: "A1", Fields: []string{"ownerId"}}}
	for i := 0; i < 5; i++ {
		if _, err := c.Resolve(context.Background(), ref); err == nil {
			t.Fatalf("call %d: expected upstream failure", i)
		}
	}
	if _, err := c.Resolve(context.Background(), ref); err != ErrUnavailable {
		t.Fatalf("expected open breaker, got %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := c.Resolve(context.Background(), ref); err != nil {
		t.Fatalf("probe after cooldown failed: %v", err)
	}
}

func TestCacheIsBoundedAndDoesNotCacheMisses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req BatchRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		results := make([]Attribute, 0, len(req.Requests))
		for _, ref := range req.Requests {
			results = append(results, Attribute{
				Type: ref.Type, ID: ref.ID,
				Found:      ref.ID != "missing",
				Attributes: map[string]any{"ownerId": "u-1"},
				TTLSeconds: 60,
			})
		}
		_ = json.NewEncoder(w).Encode(BatchResponse{Results: results})
	}))
	defer srv.Close()

	c := New(srv.URL, WithMaxCache(10))
	refs := make([]Ref, 0, 30)
	for i := 0; i < 30; i++ {
		refs = append(refs, Ref{Type: "account", ID: string(rune('A' + i)), Fields: []string{"ownerId"}})
	}
	refs = append(refs, Ref{Type: "account", ID: "missing", Fields: []string{"ownerId"}})
	if _, err := c.Resolve(context.Background(), refs); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	n := len(c.cache)
	_, negative := c.cache[cacheKey(Ref{Type: "account", ID: "missing", Fields: []string{"ownerId"}})]
	c.mu.Unlock()
	if n > 10 {
		t.Fatalf("cache unbounded: %d entries", n)
	}
	if negative {
		t.Fatal("not-found result must not be cached")
	}
}

func TestAPIVersionAndCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"apiVersion": "1.0", "build": "test"})
	}))
	defer srv.Close()
	c := New(srv.URL)
	got, err := c.APIVersion(context.Background())
	if err != nil || got != "1.0" {
		t.Fatalf("version=%q err=%v", got, err)
	}
	for _, tc := range []struct {
		expected, actual string
		ok               bool
	}{
		{"1.0", "1.0", true}, {"1", "1.4", true}, {"1.0", "1.1", false}, {"2.0", "1.9", false},
	} {
		if err := CheckAPIVersion(tc.expected, tc.actual); (err == nil) != tc.ok {
			t.Fatalf("CheckAPIVersion(%q,%q)=%v", tc.expected, tc.actual, err)
		}
	}
}

func TestMalformedResponsesFailClosed(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/attributes:batch" {
			_, _ = w.Write([]byte(`{"results":[{"found":true}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"fields":{}}`))
	}))
	defer bad.Close()
	c := New(bad.URL)
	if _, err := c.Resolve(context.Background(), []Ref{{Type: "account", ID: "A1"}}); err == nil {
		t.Fatal("expected malformed batch error")
	}
	if _, err := c.Classification(context.Background(), "account"); err == nil {
		t.Fatal("expected malformed classification error")
	}
}
