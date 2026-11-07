package integration

// BDD acceptance suite for the controls plane.
//
// The feature files in ../features are the acceptance criteria. They are
// executed against two backends with the same step definitions:
//
//   - default: the in-process engine (fast, runs in `make test`);
//   - CONTROLS_ACCEPTANCE_URL set: a live HTTP entrypoint, normally the Envoy
//     listener from `make smoke` (real ext_proc, real upstream, real masking).
//
// Scenarios tagged @engine assert implementation detail that is not visible on
// the wire (decision reasons) and are skipped by the HTTP backend. Scenarios in
// a feature tagged @e2e are expected to be observable end to end.
//
// Run: make acceptance
//      CONTROLS_ACCEPTANCE_URL=http://127.0.0.1:18000 go test ./integration -run TestAcceptance -v

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/pipclient"
	"example.com/controls/core/pip/server"
)

type world struct {
	t      *testing.T
	engine *engine.Engine
	pip    *httptest.Server

	// scenario inputs
	bundleDir      string
	httpMode       bool
	baseURL        string
	pipDown        bool
	pipClosed      bool
	upstreamBody   []byte
	upstreamStatus int
	subject        string
	claims         map[string]string
	method         string
	path           string
	reqBody        []byte

	// observed outcome
	performed bool
	status    int
	respBody  []byte
	res       engine.Result
}

type stepDef struct {
	re  *regexp.Regexp
	run func(w *world, s gherkinStep, m []string) error
}

func mustRE(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

func TestAcceptance(t *testing.T) {
	files, err := filepath.Glob("../features/*.feature")
	if err != nil {
		t.Fatalf("glob features: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no feature files found in ../features")
	}

	baseURL := strings.TrimSpace(os.Getenv("CONTROLS_ACCEPTANCE_URL"))
	for _, file := range files {
		feat, err := parseFeatureFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for _, scenario := range feat.Scenarios {
			scenario := scenario
			tags := append(append([]string{}, feat.Tags...), scenario.Tags...)
			t.Run(feat.Name+" / "+scenario.Name, func(t *testing.T) {
				if baseURL != "" {
					switch {
					case hasTag(tags, "engine"):
						t.Skip("engine-only scenario: not observable over the wire")
					case !hasTag(tags, "e2e"):
						t.Skip("scenario is not tagged @e2e")
					}
				}
				w := &world{
					t:              t,
					bundleDir:      "integration/testdata/config",
					httpMode:       baseURL != "",
					baseURL:        baseURL,
					claims:         map[string]string{},
					upstreamStatus: http.StatusOK,
				}
				steps := append(append([]gherkinStep{}, feat.Background...), scenario.Steps...)
				for _, step := range steps {
					if err := w.runStep(step); err != nil {
						t.Fatalf("%s:%d: %s %s: %v", file, step.Line, step.Keyword, step.Text, err)
					}
				}
			})
		}
	}
}

func hasTag(tags []string, want string) bool {
	want = "@" + strings.TrimPrefix(want, "@")
	for _, tag := range tags {
		if strings.TrimPrefix(tag, "@") == strings.TrimPrefix(want, "@") {
			return true
		}
	}
	return false
}

func (w *world) runStep(step gherkinStep) error {
	for _, def := range acceptanceSteps {
		if m := def.re.FindStringSubmatch(step.Text); m != nil {
			return def.run(w, step, m)
		}
	}
	return fmt.Errorf("undefined step; add it to integration/acceptance_test.go")
}

func (w *world) setupEngine() error {
	if w.engine != nil {
		return nil
	}
	repo, err := server.LoadMemoryRepo("../pip/fixtures/attributes.json")
	if err != nil {
		return fmt.Errorf("load PIP fixtures: %w", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w.pip = httptest.NewServer(server.New(repo, log).Handler())
	w.t.Cleanup(w.pip.Close)

	dir := w.bundleDir
	if _, err := os.Stat(dir); err != nil {
		dir = filepath.Join("..", dir)
	}
	bundle, err := config.LoadDir(dir)
	if err != nil {
		return fmt.Errorf("load controls bundle %q: %w", w.bundleDir, err)
	}
	w.engine = engine.New(bundle, pipclient.New(w.pip.URL))
	return nil
}

func (w *world) perform() error {
	if w.httpMode {
		return w.performHTTP()
	}
	if err := w.setupEngine(); err != nil {
		return err
	}
	if w.pipDown && !w.pipClosed {
		w.pip.Close()
		w.pipClosed = true
	}
	w.res = w.engine.ProcessRequest(context.Background(), engine.Request{
		Method:  w.method,
		Path:    w.path,
		Body:    w.reqBody,
		Subject: engine.Subject{Sub: w.subject, Claims: w.claims},
	})
	w.performed = true
	w.status = w.res.Status
	w.respBody = nil
	if !w.res.Allowed || len(w.upstreamBody) == 0 {
		return nil
	}
	masked, err := w.engine.MaskResponse(w.engine.Endpoint(w.method, w.path), w.upstreamBody, w.upstreamStatus >= 400)
	if err != nil {
		return fmt.Errorf("mask response: %w", err)
	}
	w.respBody = masked
	w.status = w.upstreamStatus
	return nil
}

func (w *world) performHTTP() error {
	var body io.Reader
	if len(w.reqBody) > 0 {
		body = bytes.NewReader(w.reqBody)
	}
	req, err := http.NewRequestWithContext(context.Background(), w.method, strings.TrimRight(w.baseURL, "/")+w.path, body)
	if err != nil {
		return err
	}
	if w.subject != "" {
		req.Header.Set("x-subject", w.subject)
	}
	for k, v := range w.claims {
		req.Header.Set("x-claim-"+k, v)
	}
	if len(w.reqBody) > 0 {
		req.Header.Set("content-type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	w.status = resp.StatusCode
	w.respBody, err = io.ReadAll(resp.Body)
	w.performed = true
	return err
}

func (w *world) requirePerformed() error {
	if !w.performed {
		return errors.New("no request has been performed; add a When step")
	}
	return nil
}

func (w *world) requireMaskedTable(s gherkinStep) error {
	if err := w.requirePerformed(); err != nil {
		return err
	}
	if len(w.respBody) == 0 {
		return errors.New("response body is empty")
	}
	var doc any
	if err := json.Unmarshal(w.respBody, &doc); err != nil {
		return fmt.Errorf("response is not JSON: %w (body %q)", err, w.respBody)
	}
	flat := map[string]string{}
	flattenJSON("", doc, flat)

	if len(s.Table) < 2 {
		return errors.New("table must have a header and at least one row")
	}
	pathCol, valueCol := -1, -1
	for i, cell := range s.Table[0] {
		switch strings.ToLower(cell) {
		case "path":
			pathCol = i
		case "value":
			valueCol = i
		}
	}
	if pathCol < 0 || valueCol < 0 {
		return fmt.Errorf("table needs path and value columns, got %v", s.Table[0])
	}
	for _, row := range s.Table[1:] {
		if pathCol >= len(row) || valueCol >= len(row) {
			return fmt.Errorf("short table row %v", row)
		}
		path, want := row[pathCol], row[valueCol]
		got, ok := flat[path]
		if !ok {
			return fmt.Errorf("%s not present in response %v", path, flat)
		}
		if got != want {
			return fmt.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	return nil
}

var acceptanceSteps = []stepDef{
	{mustRE(`^the controls bundle "([^"]+)"$`), func(w *world, _ gherkinStep, m []string) error {
		w.bundleDir = m[1]
		return nil
	}},
	{mustRE(`^the PIP serves the fixture attributes$`), func(_ *world, _ gherkinStep, _ []string) error {
		return nil
	}},
	{mustRE(`^the PIP is unavailable$`), func(w *world, _ gherkinStep, _ []string) error {
		w.pipDown = true
		return nil
	}},
	{mustRE(`^the upstream responds with:$`), func(w *world, s gherkinStep, _ []string) error {
		if strings.TrimSpace(s.DocString) == "" {
			return errors.New("missing doc string with the upstream response")
		}
		if !json.Valid([]byte(s.DocString)) {
			return fmt.Errorf("upstream response is not valid JSON: %s", s.DocString)
		}
		w.upstreamBody = []byte(s.DocString)
		return nil
	}},
	{mustRE(`^the upstream status is (\d+)$`), func(w *world, _ gherkinStep, m []string) error {
		status, err := strconv.Atoi(m[1])
		if err != nil {
			return err
		}
		w.upstreamStatus = status
		return nil
	}},
	{mustRE(`^subject "([^"]+)"$`), func(w *world, _ gherkinStep, m []string) error {
		w.subject = m[1]
		return nil
	}},
	{mustRE(`^subject "([^"]+)" with claim "([^"]+)" = "([^"]+)"$`), func(w *world, _ gherkinStep, m []string) error {
		w.subject = m[1]
		w.claims[m[2]] = m[3]
		return nil
	}},
	{mustRE(`^they (GET|POST|PUT|PATCH|DELETE) "([^"]+)"$`), func(w *world, _ gherkinStep, m []string) error {
		w.method, w.path, w.reqBody = m[1], m[2], nil
		return w.perform()
	}},
	{mustRE(`^they (GET|POST|PUT|PATCH|DELETE) "([^"]+)" with JSON:$`), func(w *world, s gherkinStep, m []string) error {
		if strings.TrimSpace(s.DocString) == "" {
			return errors.New("missing doc string with the request body")
		}
		if !json.Valid([]byte(s.DocString)) {
			return fmt.Errorf("request body is not valid JSON: %s", s.DocString)
		}
		w.method, w.path, w.reqBody = m[1], m[2], []byte(s.DocString)
		return w.perform()
	}},
	{mustRE(`^the request is allowed$`), func(w *world, _ gherkinStep, _ []string) error {
		if err := w.requirePerformed(); err != nil {
			return err
		}
		if w.httpMode {
			if w.status >= 400 {
				return fmt.Errorf("expected an allowed request, got status %d (body %q)", w.status, w.respBody)
			}
			return nil
		}
		if !w.res.Allowed {
			return fmt.Errorf("expected allow, got %+v", w.res)
		}
		return nil
	}},
	{mustRE(`^the request is denied$`), func(w *world, _ gherkinStep, _ []string) error {
		if err := w.requirePerformed(); err != nil {
			return err
		}
		if w.httpMode {
			if w.status < 400 {
				return fmt.Errorf("expected a denied request, got status %d", w.status)
			}
			return nil
		}
		if w.res.Allowed {
			return fmt.Errorf("expected deny, got %+v", w.res)
		}
		return nil
	}},
	{mustRE(`^the status is (\d+)$`), func(w *world, _ gherkinStep, m []string) error {
		if err := w.requirePerformed(); err != nil {
			return err
		}
		want, err := strconv.Atoi(m[1])
		if err != nil {
			return err
		}
		if w.status != want {
			return fmt.Errorf("status = %d, want %d", w.status, want)
		}
		return nil
	}},
	{mustRE(`^the decision reason is "([^"]+)"$`), func(w *world, _ gherkinStep, m []string) error {
		if w.httpMode {
			return nil // the reason is an internal decision, not a wire contract
		}
		if err := w.requirePerformed(); err != nil {
			return err
		}
		if w.res.Reason != m[1] {
			return fmt.Errorf("reason = %q, want %q", w.res.Reason, m[1])
		}
		return nil
	}},
	{mustRE(`^the response body is masked as:$`), func(w *world, s gherkinStep, _ []string) error {
		return w.requireMaskedTable(s)
	}},
}

func flattenJSON(prefix string, value any, out map[string]string) {
	if nested, ok := value.(map[string]any); ok {
		for key, child := range nested {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			flattenJSON(path, child, out)
		}
		return
	}
	out[prefix] = fmt.Sprint(value)
}
