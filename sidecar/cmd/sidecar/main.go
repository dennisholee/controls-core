// Command sidecar runs the controls test API.
//
// Production runs the same engine behind an Envoy ext_proc adapter. This
// command exposes it over HTTP so the solution can be tested directly:
//
//	POST /v1/process   evaluate a request and mask an upstream response
//	POST /v1/test      run a conformance fixture, with pass/fail checks
//	GET  /v1/endpoints registered routes and masking configuration
//	GET  /v1/info      bundle and runtime information
//	GET  /docs         browser console
//	GET  /openapi.yaml API contract
//
// The harness trusts x-subject/x-claim-* headers. It binds to loopback by
// default and must never be exposed to untrusted clients.
package main

import (
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/pipclient"
	"example.com/controls/core/sidecar/testapi"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8081", "listen address (loopback by default)")
	cfgDir := flag.String("config", "integration/testdata/config", "controls bundle directory")
	pipURL := flag.String("pip", "http://127.0.0.1:8082", "PIP base URL")
	fixtures := flag.String("fixtures", "", "serve the PIP in-process from this fixture file (overrides -pip)")
	maxBody := flag.Int64("max-body-bytes", testapi.DefaultMaxBodyBytes, "maximum request body")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	bundle, err := config.LoadDir(*cfgDir)
	if err != nil {
		log.Error("invalid controls bundle", "dir", *cfgDir, "err", err)
		os.Exit(1)
	}

	var (
		pip  engine.PIP
		mode string
	)
	if *fixtures != "" {
		p, err := testapi.NewLocalPIP(*fixtures)
		if err != nil {
			log.Error("load PIP fixtures", "path", *fixtures, "err", err)
			os.Exit(1)
		}
		pip = p
		mode = "fixtures"
	} else {
		pip = pipclient.New(*pipURL)
		mode = "http"
		log.Info("using external PIP", "url", *pipURL)
	}

	api := testapi.New(bundle, pip, log,
		testapi.WithBundleDir(*cfgDir),
		testapi.WithPIPMode(mode),
		testapi.WithMaxBodyBytes(*maxBody),
	)
	if err := api.StartupError(); err != nil {
		log.Error("policy engine initialisation failed", "engine", bundle.Rules.Engine, "err", err)
		os.Exit(1)
	}

	if !isLoopback(*addr) {
		log.Warn("test API derives identity from x-subject and must not be exposed",
			"addr", *addr)
	}
	log.Info("controls test API listening",
		"addr", *addr,
		"docs", "http://"+displayAddr(*addr)+"/docs",
		"config", *cfgDir,
		"pip", mode,
		"endpoints", len(bundle.Registry.Endpoints),
	)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	return addr
}
