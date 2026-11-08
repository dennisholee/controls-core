package main

import (
	"context"
	"io"
	"testing"

	"example.com/controls/core/internal/audit"
	"example.com/controls/core/internal/metrics"
	"example.com/controls/core/internal/pipclient"
)

type stubPIP struct{}

func (stubPIP) Resolve(context.Context, []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	return map[string]pipclient.Attribute{}, nil
}
func (stubPIP) Classification(context.Context, string) (map[string]string, error) {
	return nil, nil
}
func (stubPIP) ResolveFresh(context.Context, []pipclient.Ref) (map[string]pipclient.Attribute, error) {
	return map[string]pipclient.Attribute{}, nil
}
func (stubPIP) CachedClassification(string) (map[string]string, bool) { return nil, false }

func TestReloadReusesPIPClient(t *testing.T) {
	pip := stubPIP{}
	al := audit.New(audit.Config{Out: io.Discard, Salt: "test"})
	t.Cleanup(al.Close)
	reg := metrics.New()

	first, err := buildRuntime("../../../integration/testdata/config", "", "", 0, pip, al, reg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildRuntime("../../../integration/testdata/config", "", "", 0, pip, al, reg)
	if err != nil {
		t.Fatal(err)
	}
	if first.pip != pip || second.pip != pip {
		t.Fatal("reload did not keep the process-lifetime PIP client")
	}
	if first.engine == second.engine {
		t.Fatal("reload should build a new engine")
	}
	if first.fingerprint != second.fingerprint {
		t.Fatalf("fingerprint changed without a config change: %s vs %s", first.fingerprint, second.fingerprint)
	}
}

func TestReloadRejectsInvalidBundle(t *testing.T) {
	al := audit.New(audit.Config{Out: io.Discard, Salt: "test"})
	t.Cleanup(al.Close)
	if _, err := buildRuntime(t.TempDir(), "", "", 0, stubPIP{}, al, metrics.New()); err == nil {
		t.Fatal("empty bundle directory must fail to load")
	}
}
