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
	addr := flag.String("addr", ":8082", "listen address")
	fixtures := flag.String("fixtures", "pip/fixtures/attributes.json", "fixture file for the in-memory repository")
	sources := flag.String("sources", "", "HTTP attribute-source config (overrides -fixtures)")
	metricsAddr := flag.String("metrics-addr", ":9090", "metrics/health listen address (empty disables)")
	maxConcurrent := flag.Int("max-concurrent", 0, "max in-flight requests (0 = unlimited)")
	tlsCert := flag.String("tls-cert", "", "server certificate for PIP HTTPS")
	tlsKey := flag.String("tls-key", "", "server key for PIP HTTPS")
	clientCA := flag.String("client-ca", "", "CA bundle for mutual TLS client verification")
	lagSeconds := flag.Int("lag-seconds", 0, "reported replica lag on attribute responses")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	var repo server.Repository
	if *sources != "" {
		r, err := httpapi.Load(*sources)
		if err != nil {
			log.Error("load attribute sources", "err", err)
			os.Exit(1)
		}
		repo = r
		log.Info("using HTTP attribute sources", "config", *sources)
	} else {
		r, err := server.LoadMemoryRepo(*fixtures)
		if err != nil {
			log.Error("load fixtures", "err", err)
			os.Exit(1)
		}
		repo = r
	}
	reg := metrics.New()
	handler := server.New(repo, log,
		server.WithLagSeconds(*lagSeconds),
		server.WithMetrics(reg),
		server.WithMaxConcurrent(*maxConcurrent),
	).Handler()
	if *metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", reg.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		go func() {
			log.Info("pip metrics listening", "addr", *metricsAddr)
			if err := http.ListenAndServe(*metricsAddr, mux); err != nil {
				log.Error("metrics listen", "err", err)
			}
		}()
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		log.Error("tls-cert and tls-key must be set together")
		os.Exit(1)
	}
	if *tlsCert != "" {
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		if *clientCA != "" {
			raw, err := os.ReadFile(*clientCA)
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
	} else if *clientCA != "" {
		log.Error("client-ca requires tls-cert and tls-key")
		os.Exit(1)
	}
	go func() {
		log.Info("pip listening", "addr", *addr, "mtls", srv.TLSConfig != nil && srv.TLSConfig.ClientAuth != tls.NoClientCert)
		var err error
		if srv.TLSConfig != nil {
			err = srv.ListenAndServeTLS(*tlsCert, *tlsKey)
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
