// Command sidecar is the local harness for the controls engine.
//
// Production: the engine is wrapped by an Envoy ext_proc adapter and runs as a
// pod sidecar. This harness exposes the same engine over HTTP so integration
// tests and local development can exercise the full pipeline without Envoy.
//
// POST /v1/process
//
//	{"request":{"method":"GET","path":"/accounts/A123",
//	            "headers":{"x-subject":"u-1","x-claim-region":"US"}},
//	 "response":{"status":200,"body":{...}}}
package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/pipclient"
)

type envelope struct {
	Request struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	} `json:"request"`
	Response *struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	} `json:"response"`
}

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	cfgDir := flag.String("config", "integration/testdata/config", "controls bundle directory")
	pipURL := flag.String("pip", "http://127.0.0.1:8082", "PIP base URL")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	bundle, err := config.LoadDir(*cfgDir)
	if err != nil {
		log.Error("invalid controls bundle", "err", err)
		os.Exit(1)
	}
	eng := engine.New(bundle, pipclient.New(*pipURL))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /v1/process", func(w http.ResponseWriter, r *http.Request) {
		var env envelope
		if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
			http.Error(w, `{"error":"invalid_envelope"}`, http.StatusBadRequest)
			return
		}
		subject := engine.Subject{Sub: env.Request.Headers["x-subject"], Claims: map[string]string{}}
		for k, v := range env.Request.Headers {
			if strings.HasPrefix(k, "x-claim-") {
				subject.Claims[strings.TrimPrefix(k, "x-claim-")] = v
			}
		}
		res := eng.ProcessRequest(r.Context(), engine.Request{
			Method:  env.Request.Method,
			Path:    env.Request.Path,
			Body:    env.Request.Body,
			Subject: subject,
		})

		out := map[string]any{"decision": res}
		if res.Allowed && env.Response != nil && len(env.Response.Body) > 0 {
			ep := eng.Endpoint(env.Request.Method, env.Request.Path)
			masked, err := eng.MaskResponse(ep, env.Response.Body, env.Response.Status >= 400)
			if err != nil {
				http.Error(w, `{"error":"mask_failed"}`, http.StatusInternalServerError)
				return
			}
			out["response"] = map[string]any{"status": env.Response.Status, "body": json.RawMessage(masked)}
		}
		log.Info("decision", "endpoint", res.EndpointID, "resource", res.ResourceType+"/"+res.ResourceID,
			"allowed", res.Allowed, "status", res.Status, "reason", res.Reason,
			"violations", len(res.Violations), "subject", subject.Sub)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Info("sidecar harness listening", "addr", *addr, "config", *cfgDir, "pip", *pipURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
}
