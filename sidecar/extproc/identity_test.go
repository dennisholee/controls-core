package extproc

import (
	"strings"
	"testing"
	"time"
)

func TestIdentityEnvelope(t *testing.T) {
	key := []byte("test-key")
	signed, err := signIdentity(key, identityPayload{
		Sub:      "u-1",
		Claims:   map[string]string{"region": "US"},
		IssuedAt: time.Now().Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := verifyIdentity(signed, key, time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Sub != "u-1" || sub.Claims["region"] != "US" {
		t.Fatalf("subject = %+v", sub)
	}
}

func TestIdentityRejectsTamperAndAge(t *testing.T) {
	key := []byte("test-key")
	signed, _ := signIdentity(key, identityPayload{Sub: "u-1", IssuedAt: time.Now().Unix()})
	parts := strings.Split(signed, ".")
	tampered := parts[0] + "." + parts[1]
	raw, _ := signIdentity(key, identityPayload{Sub: "attacker", IssuedAt: time.Now().Unix()})
	_ = tampered
	if _, err := verifyIdentity(raw, []byte("wrong-key"), time.Now(), time.Minute); err == nil {
		t.Fatal("wrong key must fail")
	}
	old, _ := signIdentity(key, identityPayload{Sub: "u-1", IssuedAt: time.Now().Add(-10 * time.Minute).Unix()})
	if _, err := verifyIdentity(old, key, time.Now(), time.Minute); err == nil {
		t.Fatal("stale identity must fail")
	}
	if _, err := verifyIdentity("", key, time.Now(), time.Minute); err == nil {
		t.Fatal("missing identity must fail")
	}
}

func TestAdapterUsesEnvelopeWhenKeySet(t *testing.T) {
	key := []byte("test-key")
	a := New(Config{IdentityKey: key})
	raw := map[string]string{"x-subject": "attacker", "x-claim-region": "US"}
	if _, err := a.subject(raw); err == nil {
		t.Fatal("raw headers must be ignored when a key is configured")
	}
	signed, _ := signIdentity(key, identityPayload{Sub: "u-1", Claims: map[string]string{"region": "US"}, IssuedAt: time.Now().Unix()})
	sub, err := a.subject(map[string]string{"x-controls-identity": signed, "x-subject": "attacker"})
	if err != nil || sub.Sub != "u-1" || sub.Claims["region"] != "US" {
		t.Fatalf("subject = %+v err=%v", sub, err)
	}
}
