// Command extproc runs the Envoy External Processing (ext_proc) adapter with
// decision audit and Prometheus metrics.
package main

import (
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"

	"example.com/controls/core/internal/audit"
	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/metrics"
	"example.com/controls/core/internal/pipclient"
	"example.com/controls/core/internal/signing"
	"example.com/controls/core/sidecar/extproc"
)

func main() {
	listen := flag.String("listen", ":50051", "gRPC listen address")
	cfgDir := flag.String("config", "integration/testdata/config", "controls bundle directory")
	pipURL := flag.String("pip", "http://127.0.0.1:8082", "PIP base URL")
	maxBody := flag.Int("max-body-bytes", extproc.DefaultMaxBody, "max buffered request/response body")
	metricsAddr := flag.String("metrics-addr", ":9090", "metrics/health listen address (empty disables)")
	auditSample := flag.Float64("audit-sample", 0.01, "fraction of allowed decisions to audit (denials always logged)")
	auditSalt := flag.String("audit-salt", os.Getenv("CONTROLS_AUDIT_SALT"), "salt for identifier hashing (required in production)")
	bundlePubKey := flag.String("bundle-pubkey", os.Getenv("CONTROLS_BUNDLE_PUBKEY"), "Ed25519 public key for bundle signature verification")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	bundle, err := config.LoadDir(*cfgDir)
	if err != nil {
		log.Error("invalid controls bundle", "err", err)
		os.Exit(1)
	}
	if *auditSalt == "" {
		log.Error("CONTROLS_AUDIT_SALT is required; refusing to start with a predictable audit salt")
		os.Exit(1)
	}
	if *bundlePubKey != "" {
		if err := signing.VerifyDir(*cfgDir, *bundlePubKey); err != nil {
			log.Error("bundle signature verification failed", "err", err)
			os.Exit(1)
		}
		log.Info("bundle signature verified")
	} else {
		log.Warn("bundle signature not verified; set -bundle-pubkey in production")
	}

	eng := engine.New(bundle, pipclient.New(*pipURL))
	if err := eng.StartupError(); err != nil {
		log.Error("policy engine initialisation failed", "err", err)
		os.Exit(1)
	}
	registry := metrics.New()
	observer := metrics.NewObserver(registry)
	observer.Bundle(manifestValue(bundle, "version"), manifestValue(bundle, "digest"), manifestValue(bundle, "schema"))
	policyVersion := bundle.Rules.Version
	if policyVersion == "" {
		policyVersion = "1.0"
	}
	auditLog := audit.New(audit.Config{
		Out:           os.Stdout,
		Salt:          *auditSalt,
		AllowSample:   *auditSample,
		PolicyVersion: policyVersion,
		BundleVersion: manifestValue(bundle, "version"),
	})
	eng.SetHooks(engine.Hooks{OnDecision: func(d engine.Decision) {
		observer.Record(d)
		auditLog.Record(d)
	}})

	if *metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", registry.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		go func() {
			srv := &http.Server{Addr: *metricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			log.Info("metrics listening", "addr", *metricsAddr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("metrics listen", "err", err)
			}
		}()
	}

	lis, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	server := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(server, extproc.New(extproc.Config{
		Engine:       eng,
		Log:          log,
		MaxBodyBytes: *maxBody,
	}))
	go func() {
		log.Info("extproc listening", "addr", *listen, "config", *cfgDir, "pip", *pipURL)
		if err := server.Serve(lis); err != nil {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	server.GracefulStop()
}

func manifestValue(b *config.Bundle, field string) string {
	if b.Manifest == nil {
		return "unknown"
	}
	switch field {
	case "version":
		if b.Manifest.BundleVersion == "" {
			return "unknown"
		}
		return b.Manifest.BundleVersion
	case "digest":
		if b.Manifest.BundleDigest == "" {
			return "unknown"
		}
		return strings.TrimSpace(b.Manifest.BundleDigest)
	case "schema":
		if b.Manifest.SchemaVersion == "" {
			return "unknown"
		}
		return b.Manifest.SchemaVersion
	}
	return "unknown"
}
