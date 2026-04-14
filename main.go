package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ayushanandhere/GoProbe/api"
	"github.com/ayushanandhere/GoProbe/config"
	"github.com/ayushanandhere/GoProbe/logger"
	"github.com/ayushanandhere/GoProbe/monitor"
)

func main() {
	configPath := flag.String("config", "./config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.SetupLogger()

	mon := monitor.NewMonitor(cfg.Targets, log)
	mon.Start()

	apiServer := api.NewServer(cfg.Server.Port, cfg.Monitor, mon, log)

	go func() {
		if err := apiServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("API server exited", "error", err)
		}
	}()

	log.Info("GoProbe started", "port", cfg.Server.Port, "targets", len(cfg.Targets))

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	log.Info("GoProbe shutting down", "reason", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mon.Stop()
	if err := apiServer.Shutdown(ctx); err != nil {
		log.Error("HTTP server shutdown failed", "error", err)
	}

	log.Info("GoProbe stopped")
}
