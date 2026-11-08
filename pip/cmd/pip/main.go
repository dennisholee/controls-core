// Command pip runs the Policy Information Point HTTP API.
//
// Reference build: fixtures in memory. Production build: register per-domain
// Repository adapters that query read replicas with curated statements.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/controls/core/internal/metrics"
	"example.com/controls/core/pip/adapters/httpapi"
	"example.com/controls/core/pip/server"
)

func main() {
	runtimeConfig := flag.String("runtime-config", "", "PIP YAML configuration file")
	flag.String("addr", ":8082", "listen address")
	flag.String("fixtures", "pip/fixtures/attributes.json", "fixture file for the in-memory repository")
	flag.String("sources", "", "HTTP attribute-source config (overrides -fixtures)")
	flag.String("metrics-addr", ":9090", "metrics/health listen address (empty disables)")
	flag.Int("max-concurrent", 0, "max in-flight requests (0 = unlimited)")
	flag.Bool("require-caller", false, "reject API calls without a client-certificate identity")
	flag.Bool("require-mtls", false, "require TLS with a verified client certificate")
	flag.Int("rate-limit", 0, "per-caller requests/second (0 = disabled)")
	flag.Int("rate-burst", 0, "per-caller burst (defaults to rate-limit)")
	flag.String("tls-cert", "", "server certificate for PIP HTTPS")
	flag.String("tls-key", "", "server key for PIP HTTPS")
	flag.String("client-ca", "", "CA bundle for mutual TLS client verification")
	flag.Int("lag-seconds", 0, "reported replica lag on attribute responses")
	flag.Parse()

	opts, err := resolvePIPSettings(*runtimeConfig, flag.CommandLine)
	if err != nil {
		slog.Error("invalid runtime configuration", "err", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var repo server.Repository
	if opts.Sources != "" {
		r, err := httpapi.Load(opts.Sources)
		if err != nil {
			log.Error("load attribute sources", "err", err)
			os.Exit(1)
		}
		repo = r
		log.Info("using HTTP attribute sources", "config", opts.Sources)
	} else {
		r, err := server.LoadMemoryRepo(opts.Fixtures)
		if err != nil {
			log.Error("load fixtures", "err", err)
			os.Exit(1)
		}
		repo = r
	}
	reg := metrics.New()
	if opts.RequireMTLS && (opts.TLSCert == "" || opts.TLSKey == "" || opts.ClientCA == "") {
		log.Error("requireMTLS needs tlsCert, tlsKey and clientCA")
		os.Exit(1)
	}
	svc := server.New(repo, log,
		server.WithLagSeconds(opts.LagSeconds),
		server.WithMetrics(reg),
		server.WithMaxConcurrent(opts.MaxConcurrent),
		server.WithRequireCaller(opts.RequireCaller || opts.RequireMTLS),
		server.WithRateLimit(opts.RateLimit, opts.RateBurst),
	)
	handler := svc.Handler()
	if opts.MetricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", reg.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			if err := svc.Ready(r.Context()); err != nil {
				http.Error(w, "attribute sources unavailable", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("ok"))
		})
		go func() {
			log.Info("pip metrics listening", "addr", opts.MetricsAddr)
			if err := http.ListenAndServe(opts.MetricsAddr, mux); err != nil {
				log.Error("metrics listen", "err", err)
			}
		}()
	}
	srv := &http.Server{
		Addr:              opts.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	if (opts.TLSCert == "") != (opts.TLSKey == "") {
		log.Error("tlsCert and tlsKey must be set together")
		os.Exit(1)
	}
	if opts.TLSCert != "" {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		if opts.ClientCA != "" {
			raw, err := os.ReadFile(opts.ClientCA)
			if err != nil {
				log.Error("read client CA", "err", err)
				os.Exit(1)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(raw) {
				log.Error("client CA contains no certificates")
				os.Exit(1)
			}
			srv.TLSConfig.ClientCAs = pool
			srv.TLSConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
	} else if opts.ClientCA != "" {
		log.Error("client-ca requires tls-cert and tls-key")
		os.Exit(1)
	}
	go func() {
		log.Info("pip listening", "addr", opts.Addr, "mtls", srv.TLSConfig != nil && srv.TLSConfig.ClientAuth != tls.NoClientCert)
		var err error
		if srv.TLSConfig != nil {
			err = srv.ListenAndServeTLS(opts.TLSCert, opts.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
