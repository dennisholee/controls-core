package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"example.com/controls/core/internal/dq"
	"example.com/controls/core/internal/engine"
)

func decision(allowed bool) engine.Decision {
	return engine.Decision{
		RequestID:    "req-1",
		EndpointID:   "get-account",
		ResourceType: "account",
		ResourceID:   "A123",
		Subject:      "u-1",
		Action:       "read",
		Allowed:      allowed,
		Status:       200,
		Reason:       "allow:owner",
		Latency:      2 * time.Millisecond,
		PIPCache:     "hit",
		Violations:   []dq.Violation{{RuleID: "email", Field: "holder.email"}},
	}
}

func TestDenyAlwaysLoggedAllowSampled(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Out: &buf, Salt: "s", AllowSample: 0, PolicyVersion: "1.0", BundleVersion: "bundle-1"})
	l.Record(decision(true))
	if buf.Len() != 0 {
		t.Fatalf("allow should be sampled out: %s", buf.String())
	}
	l.Record(decision(false))
	if buf.Len() == 0 {
		t.Fatal("deny must always be logged")
	}
}

func TestAllowLoggedWhenFullySampled(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Out: &buf, Salt: "s", AllowSample: 1})
	l.Record(decision(true))
	if buf.Len() == 0 {
		t.Fatal("allow should be logged")
	}
}

func TestHashesNoRawIdentifiers(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Out: &buf, Salt: "s", AllowSample: 1, PolicyVersion: "1.0"})
	l.Record(decision(true))

	var e entry
	if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "u-1") || strings.Contains(buf.String(), "A123") {
		t.Fatalf("raw identifiers logged: %s", buf.String())
	}
	if len(e.SubjectHash) != 64 || len(e.ResourceIDHash) != 64 {
		t.Fatalf("unexpected hashes: %+v", e)
	}
	if e.Decision != "allow" || e.PIP == nil || e.PIP.Cache != "hit" {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if len(e.DQ) != 1 || e.DQ[0].RuleID != "email" {
		t.Fatalf("dq = %+v", e.DQ)
	}
}

func TestHashIsStablePerSalt(t *testing.T) {
	var a, b bytes.Buffer
	New(Config{Out: &a, Salt: "salt-a", AllowSample: 1}).Record(decision(true))
	New(Config{Out: &b, Salt: "salt-b", AllowSample: 1}).Record(decision(true))
	if a.String() == b.String() {
		t.Fatal("different salts should produce different hashes")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestAsyncQueueAndWriteFailures(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Out: &buf, Salt: "s", AllowSample: 1, Buffer: 16})
	l.Record(decision(true))
	l.Close()
	if buf.Len() == 0 {
		t.Fatal("async queue did not flush")
	}
	if l.Dropped() != 0 || l.WriteFailures() != 0 {
		t.Fatalf("dropped=%d failures=%d", l.Dropped(), l.WriteFailures())
	}

	bad := New(Config{Out: errWriter{}, Salt: "s", AllowSample: 1})
	bad.Record(decision(true))
	if bad.WriteFailures() != 1 {
		t.Fatalf("write failures = %d", bad.WriteFailures())
	}
}
