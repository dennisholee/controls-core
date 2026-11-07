package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

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
}

// Option configures the PIP server.
type Option func(*Server)

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

// Handler returns the HTTP handler (used directly by tests).
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleBatch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req pipclient.BatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_json"}`, http.StatusBadRequest)
		return
	}
	if len(req.Requests) == 0 {
		http.Error(w, `{"error":"empty_batch"}`, http.StatusBadRequest)
		return
	}
	if len(req.Requests) > maxBatch {
		http.Error(w, `{"error":"batch_too_large"}`, http.StatusBadRequest)
		return
	}
	results := make([]pipclient.Attribute, 0, len(req.Requests))
	for _, ref := range req.Requests {
		res, err := s.repo.Get(r.Context(), ref.Type, ref.ID, ref.Fields)
		if err != nil {
			s.log.Error("pip lookup failed", "type", ref.Type, "id", ref.ID, "err", err)
			http.Error(w, `{"error":"lookup_failed"}`, http.StatusInternalServerError)
			return
		}
		lag := res.LagSeconds
		if lag == 0 {
			lag = s.lagSeconds
		}
		results = append(results, pipclient.Attribute{
			Type:       ref.Type,
			ID:         ref.ID,
			Found:      res.Found,
			Attributes: res.Attributes,
			Version:    "fixture-v1",
			TTLSeconds: 30,
			LagSeconds: lag,
		})
	}
	s.log.Info("pip batch", "refs", len(req.Requests), "ms", time.Since(start).Milliseconds())
	writeJSON(w, http.StatusOK, pipclient.BatchResponse{Results: results})
}

func (s *Server) handleClassification(w http.ResponseWriter, r *http.Request) {
	resourceType := r.PathValue("type")
	fields, found, err := s.repo.Classification(r.Context(), resourceType)
	if err != nil {
		http.Error(w, `{"error":"lookup_failed"}`, http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, `{"error":"unknown_resource_type"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, pipclient.ClassificationResponse{
		ResourceType: resourceType,
		Fields:       fields,
		TTLSeconds:   3600,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
