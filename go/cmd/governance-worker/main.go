// The governance worker carries out deletion requests (all data of one
// person, across ClickHouse and replay storage) and enforces each project's
// retention periods, including replay objects and source maps, which have
// no table TTL.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Mutiur03/pulse/internal/artifacts"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/governance"
	"github.com/Mutiur03/pulse/internal/logging"
	"github.com/Mutiur03/pulse/internal/monitor"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

func main() {
	cfg := config.Load()
	logging.Setup("governance-worker", cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		slog.Error("governance worker stopped", "err", err)
		os.Exit(1)
	}
}

// run starts the deletion and retention loops and serves health until ctx
// ends or the health server fails.
func run(ctx context.Context, cfg *config.Config) error {
	db, err := pgxpool.New(ctx, cfg.PostgresURL)
	if err != nil {
		return fmt.Errorf("configure Postgres: %w", err)
	}
	defer db.Close()
	chClient, err := ch.Connect(cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("create ClickHouse client: %w", err)
	}
	defer func() { _ = chClient.Close() }()

	w := &governance.Worker{
		Store:     &governance.Store{DB: db},
		Analytics: chClient,
		Artifacts: &artifacts.Store{DB: db},
		Audit:     &audit.Store{DB: db},
	}
	// Without object storage, rows are still deleted; objects are left to
	// the bucket's own lifecycle rules.
	if s := replay.Connect(cfg.S3); s != nil {
		w.Replays = s
	}
	if s := replay.ConnectArtifacts(cfg.S3, cfg.ArtifactBucket); s != nil {
		w.SourceMaps = s
	}

	poll := config.Duration("DELETION_POLL_INTERVAL", 5*time.Second)
	every := config.Duration("RETENTION_INTERVAL", time.Hour)
	go repeat(ctx, poll, func() { runDeletions(ctx, w) })
	go repeat(ctx, every, func() { runRetention(ctx, w) })
	slog.Info("governance worker running", "deletion_poll", poll, "retention_every", every)
	return serveHealth(ctx, ":8084")
}

// repeat calls fn now and then every d until ctx ends.
func repeat(ctx context.Context, d time.Duration, fn func()) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		fn()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func runDeletions(ctx context.Context, w *governance.Worker) {
	if n, err := w.RunDeletions(ctx); err != nil {
		monitor.GovernanceRuns.WithLabelValues("deletions", "error").Inc()
		slog.Error("deletions", "err", err)
	} else if n > 0 {
		slog.Info("processed deletion requests", "count", n)
	}
}

func runRetention(ctx context.Context, w *governance.Worker) {
	out, err := w.EnforceRetention(ctx)
	result := "ok"
	if err != nil {
		result = "error"
		slog.Error("retention", "err", err)
	}
	monitor.GovernanceRuns.WithLabelValues("retention", result).Inc()
	for p, counts := range out {
		slog.Info("retention removed rows", "project", p, "counts", counts)
	}
}

func serveHealth(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", monitor.Handler())
	monitor.Service("governance-worker")
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy","service":"governance-worker"}`))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return fmt.Errorf("health server: %w", err)
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	return nil
}
