package server

import (
	"context"
	"crypto/x509"
	"net/http"
)

type callerKey struct{}

// WithCaller stores the authenticated caller identity in the context.
func WithCaller(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, callerKey{}, id)
}

// CallerFromContext returns the authenticated caller identity, or "" when the
// PIP is running without client-certificate authentication.
func CallerFromContext(ctx context.Context) string {
	id, _ := ctx.Value(callerKey{}).(string)
	return id
}

// CallerIdentity derives a stable identity from a client certificate: the first
// URI SAN (SPIFFE), then the first DNS SAN, then the subject CN.
func CallerIdentity(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	if len(cert.URIs) > 0 {
		return cert.URIs[0].String()
	}
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}
	return cert.Subject.CommonName
}

func callerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			var cert *x509.Certificate
			if len(r.TLS.PeerCertificates) > 0 {
				cert = r.TLS.PeerCertificates[0]
			} else if len(r.TLS.VerifiedChains) > 0 && len(r.TLS.VerifiedChains[0]) > 0 {
				cert = r.TLS.VerifiedChains[0][0]
			}
			if id := CallerIdentity(cert); id != "" {
				r = r.WithContext(WithCaller(r.Context(), id))
			}
		}
		next.ServeHTTP(w, r)
	})
}
