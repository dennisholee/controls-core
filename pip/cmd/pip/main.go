// Command pip runs the Policy Information Point HTTP API.
//
// Reference build: fixtures in memory. Production build: register per-domain
// Repository adapters that query read replicas with curated statements.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/controls/core/pip/server"
)

func main() {
	addr := flag.String("addr", ":8082", "listen address")
	fixtures := flag.String("fixtures", "pip/fixtures/attributes.json", "fixture file for the in-memory repository")
	lagSeconds := flag.Int("lag-seconds", 0, "reported replica lag on attribute responses")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	repo, err := server.LoadMemoryRepo(*fixtures)
	if err != nil {
		log.Error("load fixtures", "err", err)
		os.Exit(1)
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.New(repo, log, server.WithLagSeconds(*lagSeconds)).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go func() {
		log.Info("pip listening", "addr", *addr, "resources", repo.ResourceTypes())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
