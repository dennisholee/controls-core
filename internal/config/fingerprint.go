package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Fingerprint hashes the bundle files so the sidecar can detect a change
// without rebuilding. Missing optional files are skipped; missing required
// files are an error.
func Fingerprint(dir string) (string, error) {
	required := []string{"registry.json", "rules.json", "mask-policies.json", "dq.json"}
	optional := []string{"policies.cedar", "manifest.json", "manifest.sig"}
	h := sha256.New()
	for _, name := range required {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(raw)
		h.Write([]byte{0})
	}
	for _, name := range optional {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
