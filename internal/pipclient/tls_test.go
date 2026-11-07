package pipclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type keyPair struct {
	certPEM []byte
	keyPEM  []byte
	cert    *x509.Certificate
	key     ed25519.PrivateKey
}

func newKeyPair(t *testing.T, parent *keyPair, isCA bool, client bool) *keyPair {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "controls-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	if isCA {
		tmpl.IsCA = true
		tmpl.KeyUsage |= x509.KeyUsageCertSign
	} else if client {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		tmpl.DNSNames = []string{"localhost"}
	}
	parentCert, parentKey := tmpl, priv
	if parent != nil {
		parentCert, parentKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, pub, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &keyPair{
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		cert:    cert,
		key:     priv,
	}
}

func writePEM(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMutualTLS(t *testing.T) {
	ca := newKeyPair(t, nil, true, false)
	server := newKeyPair(t, ca, false, false)
	client := newKeyPair(t, ca, false, true)

	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"type":"account","id":"A1","found":true,"attributes":{"ownerId":"u-1"},"version":"v1","ttlSeconds":30}]}`))
	}))
	srv.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{{Certificate: [][]byte{server.cert.Raw}, PrivateKey: server.key}},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	srv.StartTLS()
	defer srv.Close()

	caPath := writePEM(t, "ca.pem", ca.certPEM)
	clientCert := writePEM(t, "client.pem", client.certPEM)
	clientKey := writePEM(t, "client-key.pem", client.keyPEM)
	cfg, err := TLSConfig(caPath, clientCert, clientKey, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	c := New(srv.URL, WithTLSConfig(cfg))
	res, err := c.Resolve(context.Background(), []Ref{{Type: "account", ID: "A1", Fields: []string{"ownerId"}}})
	if err != nil {
		t.Fatalf("mTLS resolve: %v", err)
	}
	if !res["account|A1"].Found {
		t.Fatalf("resource = %+v", res)
	}

	// Without a client certificate the server must reject the request.
	plainCfg, err := TLSConfig(caPath, "", "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(srv.URL, WithTLSConfig(plainCfg)).Resolve(context.Background(), []Ref{{Type: "account", ID: "A1"}}); err == nil {
		t.Fatal("expected mTLS failure without a client certificate")
	}
}
