package integration

// A deliberately small Gherkin reader: features live in ../features and are
// executed by acceptance_test.go. It supports the subset this repository uses
// (Feature, Background, Scenario, tags, steps, data tables and doc strings) and
// rejects the parts that would need a real parser (scenario outlines).
//
// Keeping the reader dependency-free matches the rest of the repository: the
// acceptance suite runs with the same `go test ./...` as everything else.

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type gherkinFeature struct {
	Name       string
	Tags       []string
	Background []gherkinStep
	Scenarios  []gherkinScenario
}

type gherkinScenario struct {
	Name  string
	Tags  []string
	Steps []gherkinStep
}

type gherkinStep struct {
	Keyword   string
	Text      string
	Table     [][]string
	DocString string
	Line      int
}

var gherkinStepRe = regexp.MustCompile(`^(Given|When|Then|And|But|\*)\s+(.+)$`)

func parseFeatureFile(path string) (*gherkinFeature, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	feat := &gherkinFeature{}
	var (
		pendingTags  []string
		sectionSteps []gherkinStep
		inBackground bool
		scenarioIdx  = -1
		inDoc        bool
		docLines     []string
		docTarget    int
		line         int
	)

	flush := func() {
		if scenarioIdx >= 0 {
			feat.Scenarios[scenarioIdx].Steps = sectionSteps
		} else if inBackground {
			feat.Background = sectionSteps
		}
		sectionSteps = nil
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line++
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)

		if inDoc {
			if trimmed == `"""` {
				sectionSteps[docTarget].DocString = strings.Join(docLines, "\n")
				inDoc = false
				docLines = nil
			} else {
				docLines = append(docLines, strings.TrimSpace(raw))
			}
			continue
		}

		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue

		case strings.HasPrefix(trimmed, "@"):
			pendingTags = append(pendingTags, strings.Fields(trimmed)...)

		case strings.HasPrefix(trimmed, "Feature:"):
			if feat.Name != "" {
				return nil, fmt.Errorf("%s:%d: multiple features per file are not supported", path, line)
			}
			feat.Name = strings.TrimSpace(strings.TrimPrefix(trimmed, "Feature:"))
			feat.Tags = append(feat.Tags, pendingTags...)
			pendingTags = nil

		case trimmed == "Background:":
			if scenarioIdx >= 0 {
				return nil, fmt.Errorf("%s:%d: Background must precede all scenarios", path, line)
			}
			inBackground = true
			sectionSteps = nil
			pendingTags = nil

		case strings.HasPrefix(trimmed, "Scenario Outline:"), strings.HasPrefix(trimmed, "Scenario Template:"):
			return nil, fmt.Errorf("%s:%d: scenario outlines are not supported; write explicit scenarios", path, line)

		case strings.HasPrefix(trimmed, "Scenario:"):
			if feat.Name == "" {
				return nil, fmt.Errorf("%s:%d: Scenario before Feature", path, line)
			}
			flush()
			inBackground = false
			feat.Scenarios = append(feat.Scenarios, gherkinScenario{
				Name: strings.TrimSpace(strings.TrimPrefix(trimmed, "Scenario:")),
				Tags: pendingTags,
			})
			scenarioIdx = len(feat.Scenarios) - 1
			pendingTags = nil

		case trimmed == `"""`:
			if len(sectionSteps) == 0 {
				return nil, fmt.Errorf("%s:%d: doc string without a step", path, line)
			}
			inDoc = true
			docTarget = len(sectionSteps) - 1
			docLines = nil

		case strings.HasPrefix(trimmed, "|"):
			if len(sectionSteps) == 0 {
				return nil, fmt.Errorf("%s:%d: table without a step", path, line)
			}
			last := len(sectionSteps) - 1
			sectionSteps[last].Table = append(sectionSteps[last].Table, parseTableRow(trimmed))

		default:
			if m := gherkinStepRe.FindStringSubmatch(trimmed); m != nil {
				sectionSteps = append(sectionSteps, gherkinStep{Keyword: m[1], Text: m[2], Line: line})
				continue
			}
			// Free text before the first Background or Scenario is the feature
			// narrative and is intentionally ignored.
			if feat.Name != "" && scenarioIdx < 0 && !inBackground {
				continue
			}
			return nil, fmt.Errorf("%s:%d: cannot parse %q", path, line, trimmed)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if feat.Name == "" {
		return nil, fmt.Errorf("%s: no Feature", path)
	}
	flush()
	return feat, nil
}

func parseTableRow(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
