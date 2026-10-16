package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/logging"
)

func main() {
	cfg := config.Load()
	logging.Setup("platform", cfg.LogLevel)
	slog.Info("platform combined binary starting", "service", cfg.ServiceName, "environment", cfg.Environment)

	slog.Info("this would start all services in one process")
	slog.Info("for now, run services separately")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	slog.Info("shutting down")
}
