// Command extproc runs the Envoy External Processing (ext_proc) adapter.
//
// It is the production transport for the controls engine; the HTTP harness in
// ../sidecar is for local development only.
package main

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"google.golang.org/grpc"

	"example.com/controls/core/internal/config"
	"example.com/controls/core/internal/engine"
	"example.com/controls/core/internal/pipclient"
	"example.com/controls/core/sidecar/extproc"
)

func main() {
	listen := flag.String("listen", ":50051", "gRPC listen address")
	cfgDir := flag.String("config", "integration/testdata/config", "controls bundle directory")
	pipURL := flag.String("pip", "http://127.0.0.1:8082", "PIP base URL")
	maxBody := flag.Int("max-body-bytes", extproc.DefaultMaxBody, "max buffered request/response body")
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
	eng := engine.New(bundle, pipclient.New(*pipURL))

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
