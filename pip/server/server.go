package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"example.com/controls/core/internal/metrics"
	"example.com/controls/core/internal/pipclient"
)

const (
	maxBodyBytes = 64 << 10
	maxBatch     = 100
)

// Server exposes the PIP HTTP API.
type Server struct {
	repo       Repository
	log        *slog.Logger
	mux        *http.ServeMux
	lagSeconds int
	reg        *metrics.Registry
	limit      chan struct{}
}

// Option configures the PIP server.
type Option func(*Server)

// WithMaxConcurrent bounds in-flight PIP requests. Zero means unlimited.
func WithMaxConcurrent(n int) Option {
	return func(s *Server) {
		if n > 0 {
			s.limit = make(chan struct{}, n)
		}
	}
}

// WithMetrics records request, attribute and lag metrics.
func WithMetrics(reg *metrics.Registry) Option { return func(s *Server) { s.reg = reg } }

// WithLagSeconds reports the given replica lag on every attribute response.
// Production adapters should derive this from the replica's applied position;
// the fixture implementation uses a constant for tests and smoke runs.
func WithLagSeconds(n int) Option { return func(s *Server) { s.lagSeconds = n } }

// New builds a PIP server.
func New(repo Repository, log *slog.Logger, opts ...Option) *Server {
	s := &Server{repo: repo, log: log, mux: http.NewServeMux()}
	for _, opt := range opts {
		opt(s)
	}
	s.mux.HandleFunc("POST /v1/attributes:batch", s.handleBatch)
	s.mux.HandleFunc("GET /v1/classification/{type}", s.handleClassification)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return s
}

// Handler returns the HTTP handler, with caller identity and the concurrency
// limit applied when configured.
func (s *Server) Handler() http.Handler {
	base := callerMiddleware(s.mux)
	if s.limit == nil {
		return base
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.limit <- struct{}{}:
			defer func() { <-s.limit }()
			base.ServeHTTP(w, r)
		default:
			if s.reg != nil {
				s.reg.Inc("pip_rejected_total", "PIP requests rejected before processing.",
					map[string]string{"reason": "concurrency"}, 1)
			}
			http.Error(w, `{"error":"overloaded"}`, http.StatusServiceUnavailable)
		}
	})
}

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.recordRequest("attributes")
	defer s.observe("attributes", start)
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req pipclient.BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.recordError("attributes")
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if len(req.Requests) == 0 {
		s.recordError("attributes")
		http.Error(w, `{"error":"empty_batch"}`, http.StatusBadRequest)
		return
	}
	if len(req.Requests) > maxBatch {
		s.recordError("attributes")
		http.Error(w, `{"error":"batch_too_large"}`, http.StatusBadRequest)
		return
	}
	results := make([]pipclient.Attribute, 0, len(req.Requests))
	for _, ref := range req.Requests {
		res, err := s.repo.Get(r.Context(), ref.Type, ref.ID, ref.Fields)
		if errors.Is(err, ErrForbidden) {
			s.recordError("attributes")
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		if err != nil {
			s.log.Error("pip lookup failed", "type", ref.Type, "id", ref.ID, "err", err)
			s.recordError("attributes")
			http.Error(w, `{"error":"lookup_failed"}`, http.StatusInternalServerError)
			return
		}
		lag := res.LagSeconds
		if lag == 0 {
			lag = s.lagSeconds
		}
		if s.reg != nil {
			found := "false"
			if res.Found {
				found = "true"
			}
			s.reg.Inc("pip_attributes_total", "PIP attribute resolutions.",
				map[string]string{"resource_type": ref.Type, "found": found}, 1)
			s.reg.Set("pip_lag_seconds", "Replica lag reported by the PIP.",
				map[string]string{"resource_type": ref.Type}, float64(lag))
		}
		version := res.Version
		if version == "" {
			version = "unknown"
		}
		results = append(results, pipclient.Attribute{
			Type:       ref.Type,
			ID:         ref.ID,
			Found:      res.Found,
			Attributes: res.Attributes,
			Version:    version,
			TTLSeconds: 30,
			LagSeconds: lag,
		})
	}
	s.log.Info("pip batch", "refs", len(req.Requests), "ms", time.Since(start).Milliseconds())
	writeJSON(w, http.StatusOK, pipclient.BatchResponse{Results: results})
}

func (s *Server) handleClassification(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.recordRequest("classification")
	defer s.observe("classification", start)
	resourceType := r.PathValue("type")
	fields, found, err := s.repo.Classification(r.Context(), resourceType)
	if err != nil {
		s.recordError("classification")
		http.Error(w, `{"error":"lookup_failed"}`, http.StatusInternalServerError)
		return
	}
	if !found {
		s.recordError("classification")
		http.Error(w, `{"error":"unknown_resource_type"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, pipclient.ClassificationResponse{
		ResourceType: resourceType,
		Fields:       fields,
		TTLSeconds:   3600,
	})
}

func (s *Server) recordRequest(endpoint string) {
	if s.reg == nil {
		return
	}
	s.reg.Inc("pip_requests_total", "PIP requests by endpoint.",
		map[string]string{"endpoint": endpoint}, 1)
}

func (s *Server) recordError(endpoint string) {
	if s.reg == nil {
		return
	}
	s.reg.Inc("pip_request_errors_total", "PIP request errors by endpoint.",
		map[string]string{"endpoint": endpoint}, 1)
}

func (s *Server) observe(endpoint string, start time.Time) {
	if s.reg == nil {
		return
	}
	s.reg.Observe("pip_request_duration_seconds", "PIP request latency by endpoint.",
		map[string]string{"endpoint": endpoint}, time.Since(start).Seconds())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
