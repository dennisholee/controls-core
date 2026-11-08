// Command extproc runs the Envoy External Processing (ext_proc) adapter with
// decision audit, Prometheus metrics and bundle reload.
package main

import (
	"context"
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

func waitForPIPVersion(pip *pipclient.Client, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var last error
	for {
		actual, err := pip.APIVersion(ctx)
		if err == nil {
			return actual, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return "", last
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	runtimeConfig := flag.String("runtime-config", "", "sidecar YAML configuration file")
	flag.String("listen", ":50051", "gRPC listen address")
	flag.String("config", "integration/testdata/config", "controls bundle directory")
	flag.String("pip", "http://127.0.0.1:8082", "PIP base URL")
	flag.String("pip-api-version", envDefault("PIP_API_VERSION", "1.0"), "expected PIP API major.minor (empty disables)")
	flag.Int("max-body-bytes", extproc.DefaultMaxBody, "max buffered request/response body")
	flag.String("metrics-addr", ":9090", "metrics/health listen address (empty disables)")
	flag.Float64("audit-sample", 0.01, "fraction of allowed decisions to audit (denials always logged)")
	flag.String("audit-salt", os.Getenv("CONTROLS_AUDIT_SALT"), "salt for identifier hashing (required in production)")
	flag.String("bundle-pubkey", os.Getenv("CONTROLS_BUNDLE_PUBKEY"), "Ed25519 public key for bundle signature verification")
	flag.Bool("allow-unsigned", false, "allow a bundle without -bundle-pubkey (development only)")
	flag.Int("audit-buffer", 4096, "async audit queue size (0 = synchronous)")
	flag.String("audit-out", "", "audit log file (empty = stdout)")
	flag.String("opa-url", os.Getenv("CONTROLS_OPA_URL"), "OPA base URL (required for the opa policy engine)")
	flag.String("pip-ca", os.Getenv("CONTROLS_PIP_CA"), "CA bundle for PIP TLS")
	flag.String("pip-cert", os.Getenv("CONTROLS_PIP_CERT"), "client certificate for PIP mTLS")
	flag.String("pip-key", os.Getenv("CONTROLS_PIP_KEY"), "client key for PIP mTLS")
	flag.String("pip-server-name", os.Getenv("CONTROLS_PIP_SERVER_NAME"), "expected PIP server name")
	flag.Duration("max-ttl", 5*time.Minute, "maximum PIP-advertised cache TTL")
	flag.Duration("pip-timeout", 100*time.Millisecond, "PIP request timeout")
	flag.Duration("pip-default-ttl", 30*time.Second, "PIP cache TTL when the PIP does not advertise one")
	flag.Int("pip-max-cache", 100000, "maximum cached PIP attributes")
	flag.Duration("pip-breaker-cooldown", 5*time.Second, "PIP breaker cooldown")
	flag.Duration("identity-max-skew", time.Minute, "maximum identity envelope clock skew")
	flag.String("identity-key", os.Getenv("CONTROLS_IDENTITY_HMAC_KEY"), "gateway identity HMAC key")
	flag.Duration("opa-timeout", 100*time.Millisecond, "OPA decision timeout")
	flag.Duration("reload-interval", 30*time.Second, "bundle polling interval (0 disables polling; SIGHUP still reloads)")
	flag.Bool("debug", false, "enable debug logging")
	flag.Parse()

	opts, err := resolveSettings(*runtimeConfig, flag.CommandLine)
	if err != nil {
		slog.Error("invalid runtime configuration", "err", err)
		os.Exit(1)
	}

	level := slog.LevelInfo
	if opts.Debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	if opts.AuditSalt == "" {
		log.Error("CONTROLS_AUDIT_SALT is required; refusing to start with a predictable audit salt")
		os.Exit(1)
	}
	if opts.BundlePubKey == "" {
		if !opts.AllowUnsigned {
			log.Error("bundle verification required: set -bundle-pubkey or -allow-unsigned")
			os.Exit(1)
		}
		log.Warn("bundle signature not verified: -allow-unsigned")
	}

	pipOpts := []pipclient.Option{
		pipclient.WithTimeout(opts.PIPTimeout),
		pipclient.WithDefaultTTL(opts.PIPDefaultTTL),
		pipclient.WithMaxTTL(opts.PIPMaxTTL),
		pipclient.WithMaxCache(opts.PIPMaxCache),
		pipclient.WithBreakerCooldown(opts.PIPBreakerCool),
	}
	if opts.PIPCA != "" || opts.PIPCert != "" || opts.PIPKey != "" || opts.PIPServerName != "" {
		tlsCfg, err := pipclient.TLSConfig(opts.PIPCA, opts.PIPCert, opts.PIPKey, opts.PIPServerName)
		if err != nil {
			log.Error("invalid PIP TLS configuration", "err", err)
			os.Exit(1)
		}
		pipOpts = append(pipOpts, pipclient.WithTLSConfig(tlsCfg))
	}
	pip := pipclient.New(opts.PIPURL, pipOpts...)
	registry := metrics.New()
	var current atomic.Pointer[runtimeState]
	if opts.MetricsAddr != "" {
		startMetrics(opts.MetricsAddr, registry, &current, log)
	}
	if opts.PIPAPIVersion != "" {
		actual, err := waitForPIPVersion(pip, 10*time.Second)
		if err != nil {
			log.Error("cannot read PIP API version", "err", err)
			os.Exit(1)
		}
		if err := pipclient.CheckAPIVersion(opts.PIPAPIVersion, actual); err != nil {
			registry.Inc("controls_pip_version_mismatch_total", "PIP API contract mismatches.", map[string]string{
				"expected": opts.PIPAPIVersion, "actual": actual,
			}, 1)
			log.Error("PIP API contract mismatch; holding unready for metrics/alerting",
				"err", err, "expected", opts.PIPAPIVersion, "actual", actual)
			select {}
		}
		registry.Set("controls_pip_info", "PIP API version in use.", map[string]string{"apiVersion": actual}, 1)
		log.Info("PIP API contract verified", "apiVersion", actual)
	}
	var auditWriter io.Writer = os.Stdout
	if opts.AuditOut != "" {
		f, err := os.OpenFile(opts.AuditOut, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			log.Error("open audit sink", "err", err)
			os.Exit(1)
		}
		defer f.Close()
		auditWriter = f
	}
	al := audit.New(audit.Config{
		Out:         auditWriter,
		Buffer:      opts.AuditBuffer,
		Salt:        opts.AuditSalt,
		AllowSample: opts.AuditSample,
		OnDrop: func() {
			registry.Inc("controls_audit_dropped_total", "Audit entries dropped by the async queue.", map[string]string{}, 1)
		},
		OnWriteError: func() {
			registry.Inc("controls_audit_write_failures_total", "Audit sink write failures.", map[string]string{}, 1)
		},
	})
	defer al.Close()
	rs, err := buildRuntime(opts.BundleDir, opts.BundlePubKey, opts.OPAURL, opts.OPATimeout, pip, al, registry)
	if err != nil {
		log.Error("cannot start controls bundle", "err", err)
		os.Exit(1)
	}
	current.Store(rs)
	log.Info("controls bundle loaded", "version", rs.version, "digest", rs.digest)

	lis, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	identityKey := []byte(opts.IdentityKey)
	if len(identityKey) > 0 {
		log.Info("gateway identity envelope enforced")
	} else {
		log.Warn("raw x-subject/x-claim-* identity fallback enabled; development only, set CONTROLS_IDENTITY_HMAC_KEY in production")
	}
	server := grpc.NewServer()
	extprocv3.RegisterExternalProcessorServer(server, extproc.New(extproc.Config{
		ProcessorProvider: func() extproc.Processor { return current.Load().engine },
		Log:               log,
		MaxBodyBytes:      opts.MaxBody,
		IdentityKey:       identityKey,
		IdentityMaxSkew:   opts.IdentityMaxSkew,
	}))
	go func() {
		log.Info("extproc listening", "addr", opts.Listen, "config", opts.BundleDir, "pip", opts.PIPURL)
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
		if opts.ReloadInterval > 0 {
			t := time.NewTicker(opts.ReloadInterval)
			defer t.Stop()
			tick = t.C
		}
		last := rs.fingerprint
		lastFail := ""
		reload := func(force bool, reason string) {
			fp, err := config.Fingerprint(opts.BundleDir)
			if err != nil {
				log.Error("bundle fingerprint failed", "err", err)
				return
			}
			if !force && (fp == last || fp == lastFail) {
				return
			}
			next, err := buildRuntime(opts.BundleDir, opts.BundlePubKey, opts.OPAURL, opts.OPATimeout, pip, al, registry)
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

func startMetrics(addr string, registry *metrics.Registry, current *atomic.Pointer[runtimeState], log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", registry.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		rs := current.Load()
		if rs == nil || rs.engine.StartupError() != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	go func() {
		srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		log.Info("metrics listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("metrics listen", "err", err)
		}
	}()
}

type runtimeState struct {
	engine      *engine.Engine
	pip         engine.PIP
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
	return &runtimeState{engine: eng, pip: pip, version: version, digest: digest, fingerprint: fp}, nil
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
