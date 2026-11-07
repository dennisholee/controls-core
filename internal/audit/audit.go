// Package audit emits decision logs matching contracts/decision-log.schema.json.
//
// Only hashed identifiers are written; payloads and attribute values are never
// logged. Denials are always logged, allows are sampled.
package audit

import (
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	mrand "math/rand/v2"
	"sync"
	"time"

	"example.com/controls/core/internal/engine"
)

// Config configures a Logger.
type Config struct {
	Out           io.Writer
	Salt          string
	AllowSample   float64 // 0..1
	PolicyVersion string
	BundleVersion string
	Now           func() time.Time
}

// Logger writes one JSON line per sampled decision.
type Logger struct {
	mu            sync.Mutex
	out           io.Writer
	salt          string
	allowSample   float64
	policyVersion string
	bundleVersion string
	now           func() time.Time
}

// New builds a Logger.
func New(cfg Config) *Logger {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Logger{
		out:           cfg.Out,
		salt:          cfg.Salt,
		allowSample:   cfg.AllowSample,
		policyVersion: cfg.PolicyVersion,
		bundleVersion: cfg.BundleVersion,
		now:           now,
	}
}

type pipInfo struct {
	Cache string `json:"cache,omitempty"`
}

type dqInfo struct {
	RuleID   string `json:"ruleId"`
	Field    string `json:"field"`
	Blocking bool   `json:"blocking,omitempty"`
}

type entry struct {
	TS             string   `json:"ts"`
	RequestID      string   `json:"requestId"`
	EndpointID     string   `json:"endpointId"`
	SubjectHash    string   `json:"subjectHash,omitempty"`
	ResourceType   string   `json:"resourceType,omitempty"`
	ResourceIDHash string   `json:"resourceIdHash,omitempty"`
	Action         string   `json:"action,omitempty"`
	Decision       string   `json:"decision"`
	Reason         string   `json:"reason"`
	PolicyVersion  string   `json:"policyVersion"`
	BundleVersion  string   `json:"bundleVersion,omitempty"`
	Status         int      `json:"status"`
	LatencyMs      float64  `json:"latencyMs"`
	PIP            *pipInfo `json:"pip,omitempty"`
	DQ             []dqInfo `json:"dqViolations"`
}

// Record writes a decision if it passes sampling.
func (l *Logger) Record(d engine.Decision) {
	if l == nil || l.out == nil {
		return
	}
	if d.Allowed {
		if l.allowSample <= 0 {
			return
		}
		if l.allowSample < 1 && mrand.Float64() >= l.allowSample {
			return
		}
	}
	reqID := d.RequestID
	if reqID == "" {
		reqID = randomID()
	}
	decision := "deny"
	if d.Allowed {
		decision = "allow"
	}
	e := entry{
		TS:             l.now().UTC().Format(time.RFC3339Nano),
		RequestID:      reqID,
		EndpointID:     d.EndpointID,
		SubjectHash:    l.hash(d.Subject),
		ResourceType:   d.ResourceType,
		ResourceIDHash: l.hash(d.ResourceID),
		Action:         d.Action,
		Decision:       decision,
		Reason:         d.Reason,
		PolicyVersion:  l.policyVersion,
		BundleVersion:  l.bundleVersion,
		Status:         d.Status,
		LatencyMs:      float64(d.Latency.Microseconds()) / 1000.0,
		DQ:             make([]dqInfo, 0, len(d.Violations)),
	}
	if d.PIPCache != "" {
		e.PIP = &pipInfo{Cache: d.PIPCache}
	}
	for _, v := range d.Violations {
		e.DQ = append(e.DQ, dqInfo{RuleID: v.RuleID, Field: v.Field, Blocking: v.Blocking})
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	l.mu.Lock()
	_, _ = l.out.Write(raw)
	l.mu.Unlock()
}

func (l *Logger) hash(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(l.salt + "\x00" + value))
	return hex.EncodeToString(sum[:])
}

func randomID() string {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}
