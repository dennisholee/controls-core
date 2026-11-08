// Package httpapi implements a PIP repository backed by per-domain HTTP
// attribute APIs (typically read APIs in front of replicas).
//
// The URL template, allowed fields and classification are configuration; the
// resource id is path-escaped and interpolated. No caller-controlled URL or
// field outside the allowlist can reach the upstream.
package httpapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"example.com/controls/core/pip/server"
)

// maxResponseBytes bounds one upstream attribute response.
const maxResponseBytes = 1 << 20

// Config is the source document.
type Config struct {
	Sources map[string]Source `json:"sources"`
}

// Source describes one resource type.
type Source struct {
	URL            string            `json:"url"` // must contain {id}
	TimeoutMs      int               `json:"timeoutMs"`
	Headers        map[string]string `json:"headers"`
	Fields         []string          `json:"fields"`
	Classification map[string]string `json:"classification"`
	LagHeader      string            `json:"lagHeader"`
	HealthURL      string            `json:"healthUrl"`
	VersionHeader  string            `json:"versionHeader"`
	// Caller authorization. Identities come from the PIP client certificate
	// (URI SAN, DNS SAN or CN). Empty lists mean "any authenticated caller".
	AllowedCallers []string            `json:"allowedCallers"`
	FieldCallers   map[string][]string `json:"fieldCallers"`
	// TLS options for mTLS to the attribute API. All paths are operator-owned.
	CAFile     string `json:"caFile"`
	CertFile   string `json:"certFile"`
	KeyFile    string `json:"keyFile"`
	ServerName string `json:"serverName"`
}

type source struct {
	base          *url.URL
	client        *http.Client
	headers       map[string]string
	fields        map[string]bool
	classes       map[string]string
	lagHeader     string
	versionHeader string
	allowed       map[string]bool
	fieldAuth     map[string]map[string]bool
	healthURL     string
}

// Repo implements server.Repository over HTTP attribute APIs.
type Repo struct {
	sources map[string]*source
}

// Load reads and validates a sources document.
func Load(path string) (*Repo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(cfg.Sources) == 0 {
		return nil, fmt.Errorf("%s: no sources", path)
	}
	repo := &Repo{sources: map[string]*source{}}
	for typ, src := range cfg.Sources {
		s, err := build(typ, src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", typ, err)
		}
		repo.sources[typ] = s
	}
	return repo, nil
}

func build(typ string, src Source) (*source, error) {
	u, err := url.Parse(src.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("invalid url")
	}
	if u.User != nil {
		return nil, fmt.Errorf("url must not contain credentials")
	}
	if strings.Contains(u.Host, "{id}") {
		return nil, fmt.Errorf("url must not interpolate {id} into the host")
	}
	if !strings.Contains(src.URL, "{id}") {
		return nil, fmt.Errorf("url must contain {id}")
	}
	if len(src.Fields) == 0 {
		return nil, fmt.Errorf("fields allowlist is required")
	}
	timeout := time.Duration(src.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	if timeout > 5*time.Second {
		return nil, fmt.Errorf("timeoutMs must be <= 5000")
	}
	headers := map[string]string{}
	for k, v := range src.Headers {
		expanded, err := expandEnv(v)
		if err != nil {
			return nil, fmt.Errorf("header %s: %w", k, err)
		}
		headers[k] = expanded
	}
	fields := map[string]bool{}
	for _, f := range src.Fields {
		if f == "" {
			return nil, fmt.Errorf("empty field in allowlist")
		}
		fields[f] = true
	}
	lagHeader := src.LagHeader
	if lagHeader == "" {
		lagHeader = "X-Replica-Lag-Seconds"
	}
	allowedCallers := map[string]bool{}
	for _, c := range src.AllowedCallers {
		if c == "" {
			return nil, fmt.Errorf("empty allowedCallers entry")
		}
		allowedCallers[c] = true
	}
	fieldAuth := map[string]map[string]bool{}
	for field, callers := range src.FieldCallers {
		if !fields[field] {
			return nil, fmt.Errorf("fieldCallers references unknown field %q", field)
		}
		set := map[string]bool{}
		for _, c := range callers {
			if c == "" {
				return nil, fmt.Errorf("empty caller for field %q", field)
			}
			set[c] = true
		}
		fieldAuth[field] = set
	}
	versionHeader := src.VersionHeader
	if versionHeader == "" {
		versionHeader = "X-Resource-Version"
	}
	tlsCfg, err := buildTLS(src, u.Scheme)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // attribute APIs are internal; never use an env proxy
	transport.TLSClientConfig = tlsCfg
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	return &source{
		base:          u,
		client:        &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		headers:       headers,
		fields:        fields,
		classes:       src.Classification,
		lagHeader:     lagHeader,
		versionHeader: versionHeader,
		allowed:       allowedCallers,
		fieldAuth:     fieldAuth,
		healthURL:     src.HealthURL,
	}, nil
}

func buildTLS(src Source, scheme string) (*tls.Config, error) {
	if src.CAFile == "" && src.CertFile == "" && src.KeyFile == "" && src.ServerName == "" {
		return nil, nil
	}
	if scheme != "https" {
		return nil, fmt.Errorf("tls options require an https url")
	}
	if (src.CertFile == "") != (src.KeyFile == "") {
		return nil, fmt.Errorf("certFile and keyFile must be set together")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: src.ServerName}
	if src.CAFile != "" {
		raw, err := os.ReadFile(src.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read caFile: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("caFile contains no valid certificates")
		}
		cfg.RootCAs = pool
	}
	if src.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(src.CertFile, src.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func expandEnv(v string) (string, error) {
	var missing string
	out := os.Expand(v, func(key string) string {
		val, ok := os.LookupEnv(key)
		if !ok {
			missing = key
		}
		return val
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %s is not set", missing)
	}
	return out, nil
}

// safeResourceID rejects ids that could alter the request path. The host is
// already fixed by build; this additionally prevents path traversal.
func safeResourceID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, "/\\") {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// Get fetches one resource and projects the allowlisted fields.
func (r *Repo) Get(ctx context.Context, typ, id string, fields []string) (server.Resource, error) {
	s, ok := r.sources[typ]
	if !ok {
		return server.Resource{Found: false}, nil
	}
	caller := server.CallerFromContext(ctx)
	if len(s.allowed) > 0 && !s.allowed[caller] {
		return server.Resource{}, server.ErrForbidden
	}
	want := fields
	if len(want) == 0 {
		want = make([]string, 0, len(s.fields))
		for f := range s.fields {
			want = append(want, f)
		}
	}
	for _, f := range want {
		if callers, ok := s.fieldAuth[f]; ok && !callers[caller] {
			return server.Resource{}, server.ErrForbidden
		}
	}
	if !safeResourceID(id) {
		return server.Resource{}, fmt.Errorf("invalid resource id")
	}
	target := *s.base
	escaped := url.PathEscape(id)
	target.Path = strings.ReplaceAll(target.Path, "{id}", escaped)
	target.RawQuery = strings.ReplaceAll(target.RawQuery, "{id}", escaped)
	if target.Scheme != s.base.Scheme || target.Host != s.base.Host {
		return server.Resource{}, fmt.Errorf("resource url escaped its configured origin")
	}
	reqCtx, cancel := context.WithTimeout(ctx, s.client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return server.Resource{}, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return server.Resource{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return server.Resource{Found: false}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return server.Resource{}, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return server.Resource{}, fmt.Errorf("read upstream: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return server.Resource{}, fmt.Errorf("upstream response exceeds %d bytes", maxResponseBytes)
	}
	var all map[string]any
	if err := json.Unmarshal(raw, &all); err != nil {
		return server.Resource{}, fmt.Errorf("decode upstream: %w", err)
	}
	out := map[string]any{}
	for _, f := range want {
		if !s.fields[f] {
			continue
		}
		if v, ok := all[f]; ok {
			out[f] = v
		}
	}
	lag := 0
	if raw := resp.Header.Get(s.lagHeader); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			lag = n
		}
	}
	version := strings.Trim(resp.Header.Get(s.versionHeader), `"`)
	return server.Resource{Attributes: out, Found: true, LagSeconds: lag, Version: version}, nil
}

// Classification returns the configured classification map.
// Ready probes configured source health URLs. Sources without a healthUrl are
// assumed ready; a configured probe must return 2xx.
func (r *Repo) Ready(ctx context.Context) error {
	types := make([]string, 0, len(r.sources))
	for typ := range r.sources {
		types = append(types, typ)
	}
	sort.Strings(types)
	for _, typ := range types {
		s := r.sources[typ]
		if s.healthURL == "" {
			continue
		}
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.healthURL, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("source %s: %w", typ, err)
		}
		resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
		if err != nil {
			cancel()
			return fmt.Errorf("source %s: %w", typ, err)
		}
		_ = resp.Body.Close()
		cancel()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("source %s: health status %d", typ, resp.StatusCode)
		}
	}
	return nil
}

func (r *Repo) Classification(ctx context.Context, typ string) (map[string]string, bool, error) {
	s, ok := r.sources[typ]
	if !ok {
		return nil, false, nil
	}
	caller := server.CallerFromContext(ctx)
	if len(s.allowed) > 0 && !s.allowed[caller] {
		return nil, false, server.ErrForbidden
	}
	out := make(map[string]string, len(s.classes))
	for k, v := range s.classes {
		out[k] = v
	}
	return out, true, nil
}
