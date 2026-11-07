package main

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasLevel(findings []finding, check, level string) bool {
	for _, f := range findings {
		if f.Check == check && f.Level == level {
			return true
		}
	}
	return false
}

func TestVersionDriftDetected(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "reference v1.2.3\n")
	write(t, root, "ARCHITECTURE.md", "v1.2.3\n")
	write(t, root, "docs/BDD.md", "v1.2.3\n")
	write(t, root, "CHANGELOG.md", "## [1.2.3]\n")
	write(t, root, "deploy/helm/controls-plane/Chart.yaml", `appVersion: "1.2.3"`+"\n")
	write(t, root, "deploy/helm/controls-plane/values.yaml", `tag: "9.9.9"`+"\n")
	if !hasLevel(checkVersions(root), "versions", "error") {
		t.Fatal("expected version drift")
	}
}

func TestADRIndexGap(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/adr/README.md", "- [0001](0001-one.md)\n")
	write(t, root, "docs/adr/0001-one.md", "# one\n")
	write(t, root, "docs/adr/0002-two.md", "# two\n")
	if !hasLevel(checkADRs(root), "adr", "error") {
		t.Fatal("expected orphan ADR error")
	}
}

func TestBrokenLink(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "[missing](docs/nope.md)\n")
	if !hasLevel(checkLinks(root), "links", "error") {
		t.Fatal("expected broken link error")
	}
}
