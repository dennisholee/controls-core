package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"registry.json":      `{"version":"1.0","defaultUnregistered":"deny","endpoints":[]}`,
		"rules.json":         `{"version":"1.0","defaultEffect":"deny","rules":[]}`,
		"mask-policies.json": `{"version":"1.0","policies":{}}`,
		"dq.json":            `{"version":"1.0","rules":[]}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first, err := Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.json"), []byte(`{"version":"1.0","defaultEffect":"allow","rules":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("fingerprint did not change")
	}
}
