// The governance worker carries out deletion requests (all data of one
// person, across ClickHouse and replay storage) and enforces each project's
// retention periods, including replay objects and source maps, which have
// no table TTL.
package main

import (
	"context"
	"log"
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
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

func duration(name string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(name)); err == nil && d > 0 {
		return d
	}
	return def
}

func main() {
	cfg := config.Load()
	db, err := pgxpool.New(context.Background(), cfg.PostgresURL)
	if err != nil {
		log.Fatalf("Failed to configure Postgres: %v", err)
	}
	defer db.Close()
	chClient, err := ch.NewClient(cfg.ClickHouseAddr)
	if err != nil {
		log.Fatalf("Failed to create ClickHouse client: %v", err)
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
	if s := replay.Connect(); s != nil {
		w.Replays = s
	}
	if s := replay.ConnectArtifacts(); s != nil {
		w.SourceMaps = s
	}

	poll := duration("DELETION_POLL_INTERVAL", 5*time.Second)
	every := duration("RETENTION_INTERVAL", time.Hour)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		t := time.NewTicker(poll)
		defer t.Stop()
		for {
			if n, err := w.RunDeletions(ctx); err != nil {
				log.Printf("deletions: %v", err)
			} else if n > 0 {
				log.Printf("processed %d deletion requests", n)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			out, err := w.EnforceRetention(ctx)
			if err != nil {
				log.Printf("retention: %v", err)
			}
			for p, counts := range out {
				log.Printf("retention removed for project %d: %v", p, counts)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"healthy","service":"governance-worker"}`))
	})
	srv := &http.Server{Addr: ":8084", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("health server: %v", err)
		}
	}()
	log.Printf("governance worker running (deletions every %s, retention every %s)", poll, every)
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}
