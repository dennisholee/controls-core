// Command steward runs deterministic repository-governance checks. The
// judgment layer is the repo-steward charter; this tool supplies evidence.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type finding struct {
	Level   string `json:"level"` // error | warning
	Check   string `json:"check"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

var (
	versionRE = regexp.MustCompile(`\bv(\d+\.\d+\.\d+)\b`)
	linkRE    = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)
)

func main() {
	root := flag.String("root", ".", "repository root")
	asJSON := flag.Bool("json", false, "json output")
	strict := flag.Bool("strict", true, "exit non-zero on errors")
	flag.Parse()

	findings := Run(*root)
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(findings)
	} else {
		printMarkdown(findings)
	}
	if *strict {
		for _, f := range findings {
			if f.Level == "error" {
				os.Exit(1)
			}
		}
	}
}

// Run executes every deterministic check.
func Run(root string) []finding {
	var out []finding
	out = append(out, checkDocs(root)...)
	out = append(out, checkADRs(root)...)
	out = append(out, checkVersions(root)...)
	out = append(out, checkLinks(root)...)
	out = append(out, checkImports(root)...)
	out = append(out, checkArtifacts(root)...)
	return out
}

func checkDocs(root string) []finding {
	required := []string{"README.md", "ARCHITECTURE.md", "SECURITY.md", "TESTING.md", "RUNBOOK.md", "CHANGELOG.md", "docs/agents/README.md"}
	recommended := []string{"CONTRIBUTING.md", "CODEOWNERS", "LICENSE"}
	var out []finding
	for _, p := range required {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			out = append(out, finding{Level: "error", Check: "docs", Path: p, Message: "required document missing"})
		}
	}
	for _, p := range recommended {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			out = append(out, finding{Level: "warning", Check: "docs", Path: p, Message: "recommended governance file missing"})
		}
	}
	return out
}

func checkADRs(root string) []finding {
	dir := filepath.Join(root, "docs/adr")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []finding{{Level: "error", Check: "adr", Path: "docs/adr", Message: err.Error()}}
	}
	index, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	indexed := map[string]bool{}
	for _, m := range linkRE.FindAllStringSubmatch(string(index), -1) {
		indexed[filepath.Base(m[1])] = true
	}
	var out []finding
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "README.md" {
			continue
		}
		if !indexed[name] {
			out = append(out, finding{Level: "error", Check: "adr", Path: "docs/adr/" + name, Message: "ADR not linked from docs/adr/README.md"})
		}
	}
	for name := range indexed {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			out = append(out, finding{Level: "error", Check: "adr", Path: "docs/adr/README.md", Message: "index links missing ADR " + name})
		}
	}
	return out
}

func checkVersions(root string) []finding {
	expected := ""
	if raw, err := os.ReadFile(filepath.Join(root, "VERSION")); err == nil {
		expected = strings.TrimSpace(string(raw))
	}
	specs := []struct {
		path string
		re   *regexp.Regexp
	}{
		{"README.md", versionRE},
		{"ARCHITECTURE.md", versionRE},
		{"docs/BDD.md", versionRE},
		{"deploy/helm/controls-plane/Chart.yaml", regexp.MustCompile(`appVersion:\s*"([^"]+)"`)},
		{"deploy/helm/controls-plane/values.yaml", regexp.MustCompile(`tag:\s*"([^"]+)"`)},
		{"CHANGELOG.md", regexp.MustCompile(`##\s*\[(\d+\.\d+\.\d+)\]`)},
	}
	seen := map[string][]string{}
	for _, s := range specs {
		raw, err := os.ReadFile(filepath.Join(root, s.path))
		if err != nil {
			continue
		}
		m := s.re.FindStringSubmatch(string(raw))
		if len(m) < 2 {
			continue
		}
		v := strings.TrimPrefix(m[1], "v")
		seen[v] = append(seen[v], s.path)
	}
	if expected != "" {
		seen[expected] = append(seen[expected], "VERSION")
	}
	if len(seen) <= 1 {
		return nil
	}
	var versions []string
	for v := range seen {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return []finding{{Level: "error", Check: "versions", Message: fmt.Sprintf("version drift across %v: %v", versions, seen)}}
}

func checkLinks(root string) []finding {
	var out []finding
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "dist" || name == "bin" || name == ".gocache" || name == ".gopath" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		inFence := false
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			if inFence {
				continue
			}
			for _, m := range linkRE.FindAllStringSubmatch(line, -1) {
				target := strings.TrimSpace(m[1])
				if target == "" || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
					strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "#") {
					continue
				}
				target = strings.SplitN(target, "#", 2)[0]
				if target == "" {
					continue
				}
				if _, err := os.Stat(filepath.Join(filepath.Dir(path), target)); err != nil {
					rel, _ := filepath.Rel(root, path)
					out = append(out, finding{Level: "error", Check: "links", Path: rel, Line: i + 1, Message: "broken link: " + target})
				}
			}
		}
		return nil
	})
	return out
}

func checkImports(root string) []finding {
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}|{{join .Imports \",\"}}", "./...")
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		return []finding{{Level: "warning", Check: "imports", Message: "go list failed: " + err.Error()}}
	}
	forbidden := map[string][]string{
		"internal/engine": {"sidecar/", "/pip/"},
		"internal/mask":   {"sidecar/", "/pip/"},
		"internal/dq":     {"sidecar/", "/pip/"},
		"internal/policy": {"sidecar/", "/pip/"},
		"pip/server":      {"sidecar/"},
	}
	var out []finding
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		pkg, imports := parts[0], parts[1]
		for prefix, bad := range forbidden {
			if !strings.Contains(pkg, prefix) {
				continue
			}
			for _, b := range bad {
				target := "example.com/controls/core/" + strings.TrimPrefix(b, "/")
				if strings.Contains(imports, target) {
					out = append(out, finding{Level: "error", Check: "imports", Path: pkg, Message: "forbidden import matching " + b})
				}
			}
		}
	}
	return out
}

func checkArtifacts(root string) []finding {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		return []finding{{Level: "warning", Check: "artifacts", Message: "git ls-files failed: " + err.Error()}}
	}
	var out []finding
	for _, p := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(p, "bin/") || strings.HasPrefix(p, "dist/") || strings.HasSuffix(p, ".test") || strings.HasSuffix(p, ".exe") {
			out = append(out, finding{Level: "error", Check: "artifacts", Path: p, Message: "build artifact is tracked"})
		}
	}
	return out
}

func printMarkdown(findings []finding) {
	errors, warnings := 0, 0
	for _, f := range findings {
		if f.Level == "error" {
			errors++
		} else {
			warnings++
		}
	}
	fmt.Printf("# Repository steward report\n\n")
	fmt.Printf("Errors: %d · Warnings: %d\n\n", errors, warnings)
	if len(findings) == 0 {
		fmt.Println("All deterministic checks pass.")
		return
	}
	fmt.Println("| Level | Check | Path | Line | Finding |")
	fmt.Println("|---|---|---|---|---|")
	for _, f := range findings {
		fmt.Printf("| %s | %s | %s | %d | %s |\n", f.Level, f.Check, f.Path, f.Line, f.Message)
	}
}
