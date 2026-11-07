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
	"sync/atomic"
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
	// Buffer > 0 moves writes to a bounded background queue. Record never
	// blocks; when the queue is full the entry is dropped and counted.
	Buffer       int
	OnDrop       func()
	OnWriteError func()
}

// Logger writes one JSON line per sampled decision.
type Logger struct {
	mu            sync.Mutex
	versionMu     sync.RWMutex
	out           io.Writer
	salt          string
	allowSample   float64
	policyVersion string
	bundleVersion string
	now           func() time.Time
	ch            chan []byte
	wg            sync.WaitGroup
	closeOnce     sync.Once
	dropped       atomic.Uint64
	writeFailures atomic.Uint64
	onDrop        func()
	onWriteError  func()
}

// New builds a Logger.
func New(cfg Config) *Logger {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	l := &Logger{
		out:           cfg.Out,
		salt:          cfg.Salt,
		allowSample:   cfg.AllowSample,
		policyVersion: cfg.PolicyVersion,
		bundleVersion: cfg.BundleVersion,
		now:           now,
		onDrop:        cfg.OnDrop,
		onWriteError:  cfg.OnWriteError,
	}
	if cfg.Buffer > 0 {
		l.ch = make(chan []byte, cfg.Buffer)
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			for raw := range l.ch {
				l.write(raw)
			}
		}()
	}
	return l
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
	policyVersion, bundleVersion := l.versions()
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
		PolicyVersion:  policyVersion,
		BundleVersion:  bundleVersion,
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
	if l.ch != nil {
		select {
		case l.ch <- raw:
		default:
			l.dropped.Add(1)
			if l.onDrop != nil {
				l.onDrop()
			}
		}
		return
	}
	l.write(raw)
}

func (l *Logger) write(raw []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.out.Write(raw); err != nil {
		l.writeFailures.Add(1)
		if l.onWriteError != nil {
			l.onWriteError()
		}
	}
}

// SetVersions updates the policy and bundle versions stamped on new entries.
// The logger is process-lifetime; reloads update it in place.
func (l *Logger) SetVersions(policyVersion, bundleVersion string) {
	l.versionMu.Lock()
	l.policyVersion = policyVersion
	l.bundleVersion = bundleVersion
	l.versionMu.Unlock()
}

func (l *Logger) versions() (string, string) {
	l.versionMu.RLock()
	defer l.versionMu.RUnlock()
	return l.policyVersion, l.bundleVersion
}

// Dropped reports entries dropped because the async queue was full.
func (l *Logger) Dropped() uint64 { return l.dropped.Load() }

// WriteFailures reports failed sink writes.
func (l *Logger) WriteFailures() uint64 { return l.writeFailures.Load() }

// Close drains the queue. Safe to call more than once.
func (l *Logger) Close() {
	l.closeOnce.Do(func() {
		if l.ch != nil {
			close(l.ch)
			l.wg.Wait()
		}
	})
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
