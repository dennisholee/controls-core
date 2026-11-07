// Package pipclient is the sidecar's client for the Policy Information Point.
//
// The PIP is a data service, not a decision service: it returns resource
// attributes and classification metadata. This client adds the required
// per-pod behaviour: request coalescing, TTL cache, bounded timeouts and a
// simple circuit breaker. Failures are returned to the caller, which fails
// closed.
package pipclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable is returned when the circuit breaker is open.
var ErrUnavailable = errors.New("pip unavailable")

// Ref identifies a resource and the fields the policy needs.
type Ref struct {
	Type   string   `json:"type"`
	ID     string   `json:"id"`
	Fields []string `json:"fields,omitempty"`
}

// Attribute is the PIP response for one resource.
type Attribute struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Found      bool           `json:"found"`
	Attributes map[string]any `json:"attributes"`
	Version    string         `json:"version"`
	TTLSeconds int            `json:"ttlSeconds"`
	// LagSeconds is the replica lag observed by the PIP when serving this
	// attribute. Used to enforce fresh/deny-on-stale registry entries.
	LagSeconds int `json:"lagSeconds"`
}

type BatchRequest struct {
	Requests []Ref `json:"requests"`
}

type BatchResponse struct {
	Results []Attribute `json:"results"`
}

type ClassificationResponse struct {
	ResourceType string            `json:"resourceType"`
	Fields       map[string]string `json:"fields"`
	TTLSeconds   int               `json:"ttlSeconds"`
}

type cacheEntry struct {
	attr    Attribute
	expires time.Time
}

type call struct {
	wg    sync.WaitGroup
	attrs map[string]Attribute
	err   error
}

// Client calls the PIP over HTTP.
type Client struct {
	base    string
	http    *http.Client
	ttl     time.Duration
	mu      sync.Mutex
	cache   map[string]cacheEntry
	flights map[string]*call
	fails   int
	openTil time.Time
}

// Option configures the client.
type Option func(*Client)

// WithTimeout sets the per-request timeout.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.http.Timeout = d } }

// WithDefaultTTL sets the cache TTL when the PIP does not return one.
func WithDefaultTTL(d time.Duration) Option { return func(c *Client) { c.ttl = d } }

// New builds a PIP client.
func New(base string, opts ...Option) *Client {
	c := &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{
			Timeout: 100 * time.Millisecond,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		ttl:     30 * time.Second,
		cache:   map[string]cacheEntry{},
		flights: map[string]*call{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Resolve fetches attributes for refs, using cache and coalescing misses.
func (c *Client) Resolve(ctx context.Context, refs []Ref) (map[string]Attribute, error) {
	return c.resolve(ctx, refs, false)
}

// ResolveFresh bypasses the local cache. Used for endpoints whose registry
// freshness is "fresh".
func (c *Client) ResolveFresh(ctx context.Context, refs []Ref) (map[string]Attribute, error) {
	return c.resolve(ctx, refs, true)
}

func (c *Client) resolve(ctx context.Context, refs []Ref, bypassCache bool) (map[string]Attribute, error) {
	if len(refs) == 0 {
		return map[string]Attribute{}, nil
	}
	now := time.Now()
	out := map[string]Attribute{}
	var misses []Ref

	c.mu.Lock()
	for _, r := range refs {
		key := cacheKey(r)
		if !bypassCache {
			if e, ok := c.cache[key]; ok && e.expires.After(now) {
				out[attrKey(r.Type, r.ID)] = e.attr
				continue
			}
		}
		misses = append(misses, r)
	}
	c.mu.Unlock()

	if len(misses) == 0 {
		return out, nil
	}
	flightKey := batchKey(misses)

	c.mu.Lock()
	if f, ok := c.flights[flightKey]; ok {
		c.mu.Unlock()
		f.wg.Wait()
		if f.err != nil {
			return nil, f.err
		}
		for k, v := range f.attrs {
			out[k] = v
		}
		return out, nil
	}
	f := &call{}
	f.wg.Add(1)
	c.flights[flightKey] = f
	open := time.Now().Before(c.openTil)
	c.mu.Unlock()

	if open {
		c.finish(flightKey, f, nil, ErrUnavailable)
		return nil, ErrUnavailable
	}

	fetched, err := c.fetch(ctx, misses)
	c.finish(flightKey, f, fetched, err)
	if err != nil {
		return nil, err
	}
	for k, v := range fetched {
		out[k] = v
	}
	return out, nil
}

func (c *Client) finish(key string, f *call, attrs map[string]Attribute, err error) {
	c.mu.Lock()
	delete(c.flights, key)
	if err != nil {
		c.fails++
		if c.fails >= 5 {
			c.openTil = time.Now().Add(5 * time.Second)
			c.fails = 0
		}
	} else {
		c.fails = 0
	}
	c.mu.Unlock()
	f.attrs, f.err = attrs, err
	f.wg.Done()
}

func (c *Client) fetch(ctx context.Context, refs []Ref) (map[string]Attribute, error) {
	body, err := json.Marshal(BatchRequest{Requests: refs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/attributes:batch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pip status %d", resp.StatusCode)
	}
	var out BatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	now := time.Now()
	c.mu.Lock()
	for _, r := range refs {
		for _, a := range out.Results {
			if a.Type != r.Type || a.ID != r.ID {
				continue
			}
			ttl := c.ttl
			if a.TTLSeconds > 0 {
				ttl = time.Duration(a.TTLSeconds) * time.Second
			}
			c.cache[cacheKey(r)] = cacheEntry{attr: a, expires: now.Add(ttl)}
		}
	}
	c.mu.Unlock()

	byKey := map[string]Attribute{}
	for _, a := range out.Results {
		byKey[attrKey(a.Type, a.ID)] = a
	}
	for _, r := range refs {
		if _, ok := byKey[attrKey(r.Type, r.ID)]; !ok {
			byKey[attrKey(r.Type, r.ID)] = Attribute{Type: r.Type, ID: r.ID, Found: false}
		}
	}
	return byKey, nil
}

// Classification returns the field classification map for a resource type.
func (c *Client) Classification(ctx context.Context, resourceType string) (map[string]string, error) {
	if time.Now().Before(c.openTil) {
		return nil, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/classification/"+resourceType, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pip status %d", resp.StatusCode)
	}
	var out ClassificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Fields, nil
}

// Invalidate drops all cached attributes (used by tests and by a reload hook).
func (c *Client) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[string]cacheEntry{}
}

func attrKey(typ, id string) string { return typ + "|" + id }

func cacheKey(r Ref) string {
	fields := append([]string(nil), r.Fields...)
	sort.Strings(fields)
	return attrKey(r.Type, r.ID) + "|" + strings.Join(fields, ",")
}

func batchKey(refs []Ref) string {
	keys := make([]string, 0, len(refs))
	for _, r := range refs {
		keys = append(keys, cacheKey(r))
	}
	sort.Strings(keys)
	return strings.Join(keys, ";")
}
