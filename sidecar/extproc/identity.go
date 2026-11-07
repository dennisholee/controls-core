package extproc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"example.com/controls/core/internal/engine"
)

const identityHeader = "x-controls-identity"

type identityPayload struct {
	Sub      string            `json:"sub"`
	Claims   map[string]string `json:"claims,omitempty"`
	IssuedAt int64             `json:"iat"`
}

// signIdentity is the gateway side of the envelope (used by tests and as the
// documented format for the gateway).
func signIdentity(key []byte, p identityPayload) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// verifyIdentity checks the HMAC and freshness of x-controls-identity and
// returns the subject. Raw x-subject / x-claim-* headers are ignored whenever
// a key is configured.
func verifyIdentity(value string, key []byte, now time.Time, maxSkew time.Duration) (engine.Subject, error) {
	if value == "" {
		return engine.Subject{}, errors.New("identity envelope missing")
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return engine.Subject{}, errors.New("identity envelope malformed")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return engine.Subject{}, fmt.Errorf("identity payload: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return engine.Subject{}, fmt.Errorf("identity signature: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return engine.Subject{}, errors.New("identity signature invalid")
	}
	var p identityPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return engine.Subject{}, fmt.Errorf("identity payload: %w", err)
	}
	if p.Sub == "" {
		return engine.Subject{}, errors.New("identity subject missing")
	}
	if maxSkew > 0 {
		issued := time.Unix(p.IssuedAt, 0)
		if now.Sub(issued) > maxSkew || issued.Sub(now) > maxSkew {
			return engine.Subject{}, errors.New("identity envelope expired")
		}
	}
	return engine.Subject{Sub: p.Sub, Claims: p.Claims}, nil
}
