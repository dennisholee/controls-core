// Command extproc runs the Envoy External Processing (ext_proc) adapter with
// decision audit, Prometheus metrics and bundle reload.
package main

import (
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"

	"example.com/controls/core/internal/audit"
	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/metrics"
	"example.com/controls/core/internal/pipclient"
	"example.com/controls/core/internal/policy"
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
	allowUnsigned := flag.Bool("allow-unsigned", false, "allow a bundle without -bundle-pubkey (development only)")
	auditBuffer := flag.Int("audit-buffer", 4096, "async audit queue size (0 = synchronous)")
	auditOut := flag.String("audit-out", "", "audit log file (empty = stdout)")
	opaURL := flag.String("opa-url", os.Getenv("CONTROLS_OPA_URL"), "OPA base URL (required for the opa policy engine)")
	pipCA := flag.String("pip-ca", os.Getenv("CONTROLS_PIP_CA"), "CA bundle for PIP TLS")
	pipCert := flag.String("pip-cert", os.Getenv("CONTROLS_PIP_CERT"), "client certificate for PIP mTLS")
	pipKey := flag.String("pip-key", os.Getenv("CONTROLS_PIP_KEY"), "client key for PIP mTLS")
	pipServerName := flag.String("pip-server-name", os.Getenv("CONTROLS_PIP_SERVER_NAME"), "expected PIP server name")
	maxTTL := flag.Duration("max-ttl", 5*time.Minute, "maximum PIP-advertised cache TTL")
	opaTimeout := flag.Duration("opa-timeout", 100*time.Millisecond, "OPA decision timeout")
	reloadInterval := flag.Duration("reload-interval", 30*time.Second, "bundle polling interval (0 disables polling; SIGHUP still reloads)")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	if *auditSalt == "" {
		log.Error("CONTROLS_AUDIT_SALT is required; refusing to start with a predictable audit salt")
		os.Exit(1)
	}
	if *bundlePubKey == "" {
		if !*allowUnsigned {
			log.Error("bundle verification required: set -bundle-pubkey or -allow-unsigned")
			os.Exit(1)
		}
		log.Warn("bundle signature not verified: -allow-unsigned")
	}

	pipOpts := []pipclient.Option{pipclient.WithMaxTTL(*maxTTL)}
	if *pipCA != "" || *pipCert != "" || *pipKey != "" || *pipServerName != "" {
		cfg, err := pipclient.TLSConfig(*pipCA, *pipCert, *pipKey, *pipServerName)
		if err != nil {
			log.Error("invalid PIP TLS configuration", "err", err)
			os.Exit(1)
		}
		pipOpts = append(pipOpts, pipclient.WithTLSConfig(cfg))
	}
	pip := pipclient.New(*pipURL, pipOpts...)
	registry := metrics.New()
	var auditWriter io.Writer = os.Stdout
	if *auditOut != "" {
		f, err := os.OpenFile(*auditOut, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			log.Error("open audit sink", "err", err)
			os.Exit(1)
		}
		defer f.Close()
		auditWriter = f
	}
	al := audit.New(audit.Config{
		Out:         auditWriter,
		Buffer:      *auditBuffer,
		Salt:        *auditSalt,
		AllowSample: *auditSample,
		OnDrop: func() {
			registry.Inc("controls_audit_dropped_total", "Audit entries dropped by the async queue.", map[string]string{}, 1)
		},
		OnWriteError: func() {
			registry.Inc("controls_audit_write_failures_total", "Audit sink write failures.", map[string]string{}, 1)
		},
	})
	defer al.Close()
	rs, err := buildRuntime(*cfgDir, *bundlePubKey, *opaURL, *opaTimeout, pip, al, registry)
	if err != nil {
		log.Error("cannot start controls bundle", "err", err)
		os.Exit(1)
	}
	var current atomic.Pointer[runtimeState]
	current.Store(rs)
	log.Info("controls bundle loaded", "version", rs.version, "digest", rs.digest)

	if *metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", registry.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
			if err := current.Load().engine.StartupError(); err != nil {
				http.Error(w, "policy initialisation failed", http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte("ok"))
		})
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
	identityKey := []byte(os.Getenv("CONTROLS_IDENTITY_HMAC_KEY"))
	if len(identityKey) > 0 {
		log.Info("gateway identity envelope enforced")
	}
	server := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(server, extproc.New(extproc.Config{
		EngineProvider: func() *engine.Engine { return current.Load().engine },
		Log:            log,
		MaxBodyBytes:   *maxBody,
		IdentityKey:    identityKey,
	}))
	go func() {
		log.Info("extproc listening", "addr", *listen, "config", *cfgDir, "pip", *pipURL)
		if err := server.Serve(lis); err != nil {
			log.Error("serve", "err", err)
			os.Exit(1)
		}
	}()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		var tick <-chan time.Time
		if *reloadInterval > 0 {
			t := time.NewTicker(*reloadInterval)
			defer t.Stop()
			tick = t.C
		}
		last := rs.fingerprint
		lastFail := ""
		reload := func(force bool, reason string) {
			fp, err := config.Fingerprint(*cfgDir)
			if err != nil {
				log.Error("bundle fingerprint failed", "err", err)
				return
			}
			if !force && (fp == last || fp == lastFail) {
				return
			}
			next, err := buildRuntime(*cfgDir, *bundlePubKey, *opaURL, *opaTimeout, pip, al, registry)
			if err != nil {
				lastFail = fp
				log.Error("bundle reload failed; keeping previous", "reason", reason, "err", err)
				return
			}
			old := current.Swap(next)
			last = fp
			lastFail = ""
			log.Info("bundle reloaded", "reason", reason, "version", next.version,
				"digest", next.digest, "previous", old.version)
		}
		for {
			select {
			case <-hup:
				reload(true, "SIGHUP")
			case <-tick:
				reload(false, "poll")
			}
		}
	}()

	<-stop
	server.GracefulStop()
}

type runtimeState struct {
	engine      *engine.Engine
	version     string
	digest      string
	fingerprint string
}

func buildRuntime(cfgDir, pubkey, opaURL string, opaTimeout time.Duration, pip engine.PIP, al *audit.Logger, reg *metrics.Registry) (*runtimeState, error) {
	bundle, err := config.LoadDir(cfgDir)
	if err != nil {
		return nil, err
	}
	if pubkey != "" {
		if err := signing.VerifyDir(cfgDir, pubkey); err != nil {
			return nil, err
		}
	}
	eng := engine.NewWithOptions(bundle, pip, engine.Options{
		Policy: policy.Options{OPAURL: opaURL, OPATimeout: opaTimeout},
	})
	if err := eng.StartupError(); err != nil {
		return nil, err
	}
	observer := metrics.NewObserver(reg)
	version, digest, schema := manifestValue(bundle, "version"), manifestValue(bundle, "digest"), manifestValue(bundle, "schema")
	reg.Reset("controls_bundle_info")
	observer.Bundle(version, digest, schema)
	policyVersion := bundle.Rules.Version
	if policyVersion == "" {
		policyVersion = "1.0"
	}
	al.SetVersions(policyVersion, version)
	eng.SetHooks(engine.Hooks{OnDecision: func(d engine.Decision) {
		observer.Record(d)
		al.Record(d)
	}})
	fp, err := config.Fingerprint(cfgDir)
	if err != nil {
		return nil, err
	}
	return &runtimeState{engine: eng, version: version, digest: digest, fingerprint: fp}, nil
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
