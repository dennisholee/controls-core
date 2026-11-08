package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func pipFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("addr", ":8082", "")
	fs.String("fixtures", "pip/fixtures/attributes.json", "")
	fs.String("sources", "", "")
	fs.String("metrics-addr", ":9090", "")
	fs.Int("max-concurrent", 0, "")
	fs.Bool("require-caller", false, "")
	fs.Bool("require-mtls", false, "")
	fs.Int("rate-limit", 0, "")
	fs.Int("rate-burst", 0, "")
	fs.String("tls-cert", "", "")
	fs.String("tls-key", "", "")
	fs.String("client-ca", "", "")
	fs.Int("lag-seconds", 0, "")
	return fs
}

func writePIPConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pip.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const pipTestConfig = `
server:
  addr: ":6001"
  metricsAddr: ":7001"
  maxConcurrent: 256
  requireCaller: true
  requireMTLS: true
  tlsCert: /tls/server.pem
  tlsKey: /tls/server-key.pem
  clientCA: /tls/ca.pem
  rateLimit: 100
  rateBurst: 20
  lagSeconds: 3
repository:
  fixtures: /fixtures.json
`

func TestLoadAndResolvePIPConfig(t *testing.T) {
	path := writePIPConfig(t, pipTestConfig)
	fs := pipFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	got, err := resolvePIPSettings(path, fs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Addr != ":6001" || got.MetricsAddr != ":7001" || got.MaxConcurrent != 256 ||
		!got.RequireCaller || !got.RequireMTLS || got.TLSCert != "/tls/server.pem" ||
		got.TLSKey != "/tls/server-key.pem" || got.ClientCA != "/tls/ca.pem" ||
		got.RateLimit != 100 || got.RateBurst != 20 || got.LagSeconds != 3 {
		t.Fatalf("settings = %+v", got)
	}
	if got.Fixtures != "/fixtures.json" || got.Sources != "" {
		t.Fatalf("repository = %+v", got)
	}
}

func TestPIPFlagBeatsFile(t *testing.T) {
	path := writePIPConfig(t, pipTestConfig)
	fs := pipFlagSet()
	if err := fs.Parse([]string{"-addr", ":9001", "-require-caller=false"}); err != nil {
		t.Fatal(err)
	}
	got, err := resolvePIPSettings(path, fs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Addr != ":9001" || got.RequireCaller {
		t.Fatalf("flags did not win: %+v", got)
	}
}

func TestPIPRejectsUnknownField(t *testing.T) {
	path := writePIPConfig(t, "server:\n  nope: 1\n")
	fs := pipFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePIPSettings(path, fs); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestPIPRejectsMTLSWithoutMaterial(t *testing.T) {
	path := writePIPConfig(t, "server:\n  requireMTLS: true\n")
	fs := pipFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePIPSettings(path, fs); err == nil {
		t.Fatal("expected mTLS validation error")
	}
}
