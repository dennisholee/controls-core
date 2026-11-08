package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func configFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("listen", ":50051", "")
	fs.String("config", "integration/testdata/config", "")
	fs.String("pip", "http://127.0.0.1:8082", "")
	fs.Int("max-body-bytes", 1<<20, "")
	fs.String("metrics-addr", ":9090", "")
	fs.Float64("audit-sample", 0.01, "")
	fs.String("audit-salt", "", "")
	fs.String("bundle-pubkey", "", "")
	fs.Bool("allow-unsigned", false, "")
	fs.Int("audit-buffer", 4096, "")
	fs.String("audit-out", "", "")
	fs.String("opa-url", "", "")
	fs.String("pip-ca", "", "")
	fs.String("pip-cert", "", "")
	fs.String("pip-key", "", "")
	fs.String("pip-server-name", "", "")
	fs.Duration("max-ttl", 5*time.Minute, "")
	fs.Duration("pip-timeout", 100*time.Millisecond, "")
	fs.Duration("pip-default-ttl", 30*time.Second, "")
	fs.Int("pip-max-cache", 100000, "")
	fs.Duration("pip-breaker-cooldown", 5*time.Second, "")
	fs.Duration("identity-max-skew", time.Minute, "")
	fs.String("identity-key", "", "")
	fs.Duration("opa-timeout", 100*time.Millisecond, "")
	fs.Duration("reload-interval", 30*time.Second, "")
	fs.Bool("debug", false, "")
	return fs
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sidecar.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const testConfig = `
server:
  listen: ":6000"
  metricsAddr: ":7000"
  maxBodyBytes: 2048
  debug: true
bundle:
  path: /bundle
  allowUnsigned: true
  reloadInterval: 45s
pip:
  url: http://pip:8082
  timeout: 250ms
  defaultTTL: 20s
  maxTTL: 2m
  maxCache: 1234
  breakerCooldown: 7s
policy:
  opa:
    url: http://opa:8181
    timeout: 150ms
identity:
  maxSkew: 90s
audit:
  sample: 0.5
  buffer: 10
  salt: file-salt
`

func TestLoadAndResolveSidecarConfig(t *testing.T) {
	path := writeConfig(t, testConfig)
	fs := configFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSettings(path, fs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Listen != ":6000" || got.MetricsAddr != ":7000" || got.MaxBody != 2048 || !got.Debug {
		t.Fatalf("server = %+v", got)
	}
	if got.BundleDir != "/bundle" || !got.AllowUnsigned || got.ReloadInterval != 45*time.Second {
		t.Fatalf("bundle = %+v", got)
	}
	if got.PIPURL != "http://pip:8082" || got.PIPTimeout != 250*time.Millisecond ||
		got.PIPDefaultTTL != 20*time.Second || got.PIPMaxTTL != 2*time.Minute ||
		got.PIPMaxCache != 1234 || got.PIPBreakerCool != 7*time.Second {
		t.Fatalf("pip = %+v", got)
	}
	if got.OPAURL != "http://opa:8181" || got.OPATimeout != 150*time.Millisecond {
		t.Fatalf("opa = %+v", got)
	}
	if got.IdentityMaxSkew != 90*time.Second || got.AuditSample != 0.5 || got.AuditBuffer != 10 || got.AuditSalt != "file-salt" {
		t.Fatalf("identity/audit = %+v", got)
	}
}

func TestResolvePrecedence(t *testing.T) {
	path := writeConfig(t, testConfig)
	t.Setenv("CONTROLS_AUDIT_SALT", "env-salt")
	t.Setenv("CONTROLS_PIP_CA", "/env/ca.pem")
	fs := configFlagSet()
	if err := fs.Parse([]string{"-listen", ":9000", "-audit-salt", "flag-salt"}); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSettings(path, fs)
	if err != nil {
		t.Fatal(err)
	}
	// Explicit flags beat env and file.
	if got.Listen != ":9000" || got.AuditSalt != "flag-salt" {
		t.Fatalf("flags did not win: %+v", got)
	}
	// Env beats file when the flag is not explicit.
	if got.PIPCA != "/env/ca.pem" {
		t.Fatalf("env did not beat file: %+v", got.PIPCA)
	}
	// File beats defaults.
	if got.PIPURL != "http://pip:8082" || got.PIPMaxCache != 1234 {
		t.Fatalf("file did not beat defaults: %+v", got)
	}
}

func TestFileSecretEnvExpansion(t *testing.T) {
	t.Setenv("SIDECAR_TEST_SALT", "expanded")
	path := writeConfig(t, "audit:\n  salt: ${SIDECAR_TEST_SALT}\n")
	fs := configFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSettings(path, fs)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuditSalt != "expanded" {
		t.Fatalf("salt = %q", got.AuditSalt)
	}
}

func TestFileSecretSchemes(t *testing.T) {
	t.Setenv("SIDECAR_FILE_SALT", "from-env")
	secretPath := filepath.Join(t.TempDir(), "salt")
	if err := os.WriteFile(secretPath, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, "audit:\n  salt: env:SIDECAR_FILE_SALT\nidentity:\n  hmacKey: file:"+secretPath+"\n")
	fs := configFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	got, err := resolveSettings(path, fs)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuditSalt != "from-env" || got.IdentityKey != "from-file" {
		t.Fatalf("audit=%q identity=%q", got.AuditSalt, got.IdentityKey)
	}
}

func TestRejectUnknownField(t *testing.T) {
	path := writeConfig(t, "server:\n  listen: :1\nunknown: true\n")
	fs := configFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSettings(path, fs); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestRejectInvalidValues(t *testing.T) {
	path := writeConfig(t, "audit:\n  sample: 2\n")
	fs := configFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSettings(path, fs); err == nil {
		t.Fatal("expected audit sample validation error")
	}
}
