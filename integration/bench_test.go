package integration

import (
	"context"
	"strings"
	"testing"

	"example.com/controls/core/internal/engine"
)

func paymentBody() []byte {
	padding := strings.Repeat("x", 3500)
	return []byte(`{"_type":"payment","_id":"P-9001","amount":120.5,` +
		`"currency":"USD","ownerId":"u-1","region":"US","padding":"` + padding + `"}`)
}

// BenchmarkProcessRequest measures the full decision path including the local
// PIP HTTP call on a cold cache. The target gate is <=3 ms p99 and <=0.2 vCPU
// per 1k RPS at 1,200 RPS/pod; this benchmark is the regression signal.
func BenchmarkProcessRequest(b *testing.B) {
	eng, _ := newEngine(b)
	body := paymentBody()
	req := engine.Request{
		Method:  "POST",
		Path:    "/payments",
		Body:    body,
		Subject: engine.Subject{Sub: "u-1", Claims: map[string]string{"region": "US"}},
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := eng.ProcessRequest(ctx, req)
		if !res.Allowed {
			b.Fatalf("unexpected deny: %+v", res)
		}
	}
}

// BenchmarkMaskResponse measures response masking on a 4 KB document.
func BenchmarkMaskResponse(b *testing.B) {
	eng, _ := newEngine(b)
	body := []byte(`{"accountNumber":"1234567890","holder":{"email":"jane.doe@example.com"},` +
		`"ssn":"111-22-3333","region":"US","note":"` + strings.Repeat("y", 3800) + `"}`)
	ep := eng.Endpoint("GET", "/accounts/A123")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.MaskResponse(ep, body, false); err != nil {
			b.Fatal(err)
		}
	}
	_ = ctx
}

// BenchmarkProcessRequestWithPIP measures the read path including the local PIP
// HTTP call (warm cache after the first iteration).
func BenchmarkProcessRequestWithPIP(b *testing.B) {
	eng, _ := newEngine(b)
	req := engine.Request{
		Method:  "GET",
		Path:    "/accounts/A123",
		Subject: engine.Subject{Sub: "u-1", Claims: map[string]string{"region": "US"}},
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := eng.ProcessRequest(ctx, req)
		if !res.Allowed {
			b.Fatalf("unexpected deny: %+v", res)
		}
	}
}
