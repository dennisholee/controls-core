package signing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"example.com/controls/core/internal/bundle"
)

func testBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"rules.json":         `{"version":"1.0","defaultEffect":"deny","rules":[]}`,
		"mask-policies.json": `{"version":"1.0","policies":{}}`,
		"registry.json":      `{"version":"1.0","defaultUnregistered":"deny","endpoints":[]}`,
		"dq.json":            `{"version":"1.0","rules":[]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := bundle.DigestDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{"schemaVersion": "1.0", "bundleDigest": digest, "bundleVersion": "bundle-test"}
	raw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSignAndVerify(t *testing.T) {
	dir := testBundle(t)
	keys := t.TempDir()
	_, pub, err := GenerateKeyPair(keys)
	if err != nil {
		t.Fatal(err)
	}
	priv := filepath.Join(keys, DefaultKeyFile)
	if err := SignDir(dir, priv); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDir(dir, pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestTamperedBundleFails(t *testing.T) {
	dir := testBundle(t)
	keys := t.TempDir()
	_, pub, _ := GenerateKeyPair(keys)
	if err := SignDir(dir, filepath.Join(keys, DefaultKeyFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.json"), []byte(`{"version":"1.0","defaultEffect":"allow","rules":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDir(dir, pub); err == nil {
		t.Fatal("tampered bundle should fail verification")
	}
}

func TestMissingSignatureFails(t *testing.T) {
	dir := testBundle(t)
	keys := t.TempDir()
	_, pub, _ := GenerateKeyPair(keys)
	if err := VerifyDir(dir, pub); err == nil {
		t.Fatal("missing signature should fail")
	}
}

func TestWrongKeyFails(t *testing.T) {
	dir := testBundle(t)
	keys := t.TempDir()
	_, _, _ = GenerateKeyPair(keys)
	elsewhere := t.TempDir()
	_, otherPub, _ := GenerateKeyPair(elsewhere)
	if err := SignDir(dir, filepath.Join(keys, DefaultKeyFile)); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDir(dir, otherPub); err == nil {
		t.Fatal("wrong public key should fail")
	}
}
