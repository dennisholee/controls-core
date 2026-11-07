// Package metrics exposes the controls plane as Prometheus text metrics,
// without pulling a client library into the sidecar.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// DefaultBuckets suits sub-millisecond to multi-second control latency.
var DefaultBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

type hist struct {
	counts []uint64
	sum    float64
	count  uint64
}

type family struct {
	help    string
	typ     string
	values  map[string]float64
	labels  map[string]map[string]string
	hists   map[string]*hist
	buckets []float64
}

// Registry is a small, concurrency-safe metric store.
type Registry struct {
	mu       sync.Mutex
	families map[string]*family
}

// New builds an empty registry.
func New() *Registry {
	return &Registry{families: map[string]*family{}}
}

func (r *Registry) family(name, help, typ string) *family {
	f, ok := r.families[name]
	if !ok {
		f = &family{
			help:    help,
			typ:     typ,
			values:  map[string]float64{},
			labels:  map[string]map[string]string{},
			hists:   map[string]*hist{},
			buckets: DefaultBuckets,
		}
		r.families[name] = f
	}
	return f
}

// Inc adds delta to a counter.
func (r *Registry) Inc(name, help string, labels map[string]string, delta float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.family(name, help, "counter")
	key := seriesKey(labels)
	f.labels[key] = labels
	f.values[key] += delta
}

// Set sets a gauge.
func (r *Registry) Set(name, help string, labels map[string]string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.family(name, help, "gauge")
	key := seriesKey(labels)
	f.labels[key] = labels
	f.values[key] = value
}

// Observe records a histogram observation.
func (r *Registry) Observe(name, help string, labels map[string]string, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f := r.family(name, help, "histogram")
	key := seriesKey(labels)
	f.labels[key] = labels
	h := f.hists[key]
	if h == nil {
		h = &hist{counts: make([]uint64, len(f.buckets))}
		f.hists[key] = h
	}
	for i, b := range f.buckets {
		if value <= b {
			h.counts[i]++
		}
	}
	h.sum += value
	h.count++
}

// Handler serves the registry in Prometheus text format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.Write(w)
	})
}

// Write renders all metrics deterministically.
func (r *Registry) Write(w io.Writer) {
	r.mu.Lock()
	names := make([]string, 0, len(r.families))
	for n := range r.families {
		names = append(names, n)
	}
	sort.Strings(names)
	type snapshot struct {
		name string
		f    family
	}
	snaps := make([]snapshot, 0, len(names))
	for _, n := range names {
		f := *r.families[n]
		f.values = cloneValues(f.values)
		f.labels = cloneLabels(f.labels)
		f.hists = cloneHists(f.hists)
		snaps = append(snaps, snapshot{name: n, f: f})
	}
	r.mu.Unlock()

	for _, s := range snaps {
		f := s.f
		fmt.Fprintf(w, "# HELP %s %s\n", s.name, f.help)
		fmt.Fprintf(w, "# TYPE %s %s\n", s.name, f.typ)
		keys := make([]string, 0, len(f.labels))
		for k := range f.labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			labels := f.labels[k]
			switch f.typ {
			case "histogram":
				h := f.hists[k]
				if h == nil {
					continue
				}
				for i, b := range f.buckets {
					fmt.Fprintf(w, "%s_bucket%s %d\n", s.name, labelsWith(labels, "le", formatFloat(b)), h.counts[i])
				}
				fmt.Fprintf(w, "%s_bucket%s %d\n", s.name, labelsWith(labels, "le", "+Inf"), h.count)
				fmt.Fprintf(w, "%s_sum%s %s\n", s.name, labelString(labels), formatFloat(h.sum))
				fmt.Fprintf(w, "%s_count%s %d\n", s.name, labelString(labels), h.count)
			default:
				fmt.Fprintf(w, "%s%s %s\n", s.name, labelString(labels), formatFloat(f.values[k]))
			}
		}
	}
}

func cloneValues(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneLabels(in map[string]map[string]string) map[string]map[string]string {
	out := make(map[string]map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneHists(in map[string]*hist) map[string]*hist {
	out := make(map[string]*hist, len(in))
	for k, v := range in {
		copyCounts := append([]uint64(nil), v.counts...)
		out[k] = &hist{counts: copyCounts, sum: v.sum, count: v.count}
	}
	return out
}

func seriesKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
		b.WriteByte(',')
	}
	return b.String()
}

func labelString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Quote(labels[k]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func labelsWith(labels map[string]string, key, value string) string {
	merged := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		merged[k] = v
	}
	merged[key] = value
	return labelString(merged)
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// Reset drops a metric family. Used when the bundle info gauge is replaced
// after a reload so stale label sets do not linger.
func (r *Registry) Reset(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.families, name)
}
