// Package signing provides detached Ed25519 signatures for rendered bundles.
//
// The pipeline signs the bundle digest with a private key held in Jenkins
// credentials; the sidecar verifies it with the matching public key before
// serving. Verification recomputes the digest from the four bundle files, so
// modifying a file invalidates the signature even if manifest.json is updated.
package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"example.com/controls/core/internal/bundle"
)

// SignatureFile is the detached signature written next to manifest.json.
const SignatureFile = "manifest.sig"

// DefaultKeyFile and DefaultPublicKeyFile are written by GenerateKeyPair.
const (
	DefaultKeyFile       = "bundle-signing.pem"
	DefaultPublicKeyFile = "bundle-signing.pub.pem"
)

type manifest struct {
	BundleDigest string `json:"bundleDigest"`
}

// GenerateKeyPair writes an Ed25519 private/public key pair to dir.
func GenerateKeyPair(dir string) (string, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	privPath := filepath.Join(dir, DefaultKeyFile)
	pubPath := filepath.Join(dir, DefaultPublicKeyFile)
	if err := os.WriteFile(privPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o644); err != nil {
		return "", "", err
	}
	return privPath, pubPath, nil
}

// SignDir signs the recomputed bundle digest and writes manifest.sig.
func SignDir(dir, keyPath string) error {
	digest, err := bundle.DigestDir(dir)
	if err != nil {
		return fmt.Errorf("recompute digest: %w", err)
	}
	recorded, err := readDigest(dir)
	if err != nil {
		return err
	}
	if recorded != digest {
		return fmt.Errorf("manifest digest %s does not match bundle %s", recorded, digest)
	}
	key, err := LoadPrivateKey(keyPath)
	if err != nil {
		return err
	}
	payload, err := signingPayload(dir, digest)
	if err != nil {
		return err
	}
	sig := ed25519.Sign(key, payload)
	return os.WriteFile(filepath.Join(dir, SignatureFile), []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}

// VerifyDir verifies manifest.sig against the recomputed digest.
func VerifyDir(dir, pubPath string) error {
	digest, err := bundle.DigestDir(dir)
	if err != nil {
		return fmt.Errorf("recompute digest: %w", err)
	}
	recorded, err := readDigest(dir)
	if err != nil {
		return err
	}
	if recorded != digest {
		return fmt.Errorf("bundle digest mismatch: manifest %s, files %s", recorded, digest)
	}
	raw, err := os.ReadFile(filepath.Join(dir, SignatureFile))
	if err != nil {
		return fmt.Errorf("read signature: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(string(trimNewline(raw)))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	pub, err := LoadPublicKey(pubPath)
	if err != nil {
		return err
	}
	payload, err := signingPayload(dir, digest)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return fmt.Errorf("bundle signature does not verify")
	}
	return nil
}

// signingPayload binds both the file digest and the complete manifest (which
// carries the environment, service, bundle version and contract version).
func signingPayload(dir, digest string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return []byte(digest + "\n" + hex.EncodeToString(sum[:])), nil
}

// LoadPrivateKey reads a PKCS#8 Ed25519 private key.
func LoadPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%s: no PEM block", path)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ed, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an Ed25519 private key", path)
	}
	return ed, nil
}

// LoadPublicKey reads a PKIX Ed25519 public key.
func LoadPublicKey(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("%s: no PEM block", path)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ed, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an Ed25519 public key", path)
	}
	return ed, nil
}

func readDigest(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", fmt.Errorf("read manifest: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", fmt.Errorf("parse manifest: %w", err)
	}
	if m.BundleDigest == "" {
		return "", fmt.Errorf("manifest has no bundleDigest")
	}
	return m.BundleDigest, nil
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
