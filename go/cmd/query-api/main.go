package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/api"
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/artifacts"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/orgs"
	"github.com/Mutiur03/pulse/internal/projects"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
	"github.com/Mutiur03/pulse/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	cfg.ServiceName = "query-api"

	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Printf("Failed to initialize tracer: %v", err)
	} else {
		defer func() { _ = cleanup(context.Background()) }()
	}

	chClient, err := ch.NewClient(cfg.ClickHouseAddr)
	if err != nil {
		log.Fatalf("Failed to create ClickHouse client: %v", err)
	}
	defer func() { _ = chClient.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	issueStore, err := issues.Connect(ctx, cfg.PostgresURL)
	cancel()
	if err != nil {
		log.Fatalf("Failed to connect to Postgres: %v", err)
	}
	defer issueStore.Close()

	db, err := pgxpool.New(context.Background(), cfg.PostgresURL)
	if err != nil {
		log.Fatalf("Failed to configure Postgres: %v", err)
	}
	defer db.Close()
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer func() { _ = rdb.Close() }()
	accounts := &auth.Store{DB: db}
	srv := &api.Server{
		Traces: chClient, Issues: issueStore, Accounts: accounts,
		Keys:     &keys.Store{DB: db, Redis: rdb},
		Projects: &projects.Store{DB: db},
		// Five failed logins per email and address lock it for 15 minutes.
		Throttle: &auth.Throttle{Redis: rdb, Max: 5, Window: 15 * time.Minute},
		// Five reset emails per address and client per hour.
		ResetThrottle: &auth.Throttle{Redis: rdb, Max: 5, Window: time.Hour, Prefix: "reset:req"},
		Orgs:          &orgs.Store{DB: db},
		// Without SMTP_ADDR emails are written to the log.
		Mail: mail.New(mail.Config{
			Addr: os.Getenv("SMTP_ADDR"), From: os.Getenv("SMTP_FROM"),
			User: os.Getenv("SMTP_USER"), Password: os.Getenv("SMTP_PASSWORD"),
		}),
		AppURL:  os.Getenv("APP_URL"),
		Replays: chClient,
		Metrics: chClient,
		// Set COOKIE_SECURE=false only for plain-HTTP local development.
		SecureCookie: os.Getenv("COOKIE_SECURE") != "false",
	}

	srv.Alerts, srv.AlertMetrics = &alerts.Store{DB: db}, chClient
	srv.AlertSender = &alerts.Notifier{Mail: srv.Mail, AppURL: srv.AppURL}

	// A typed nil would look non-nil through the interface.
	if objects := replay.Connect(); objects != nil {
		srv.ReplayObjects = objects
	}
	// Releases and upload tokens work without object storage; uploads and
	// symbolication need it.
	rel := &artifacts.Store{DB: db}
	srv.Releases = rel
	if objects := replay.ConnectArtifacts(); objects != nil {
		srv.ArtifactObjects = objects
		srv.Symbolicator = &symbolicate.Symbolicator{Artifacts: rel, Objects: objects}
	}
	if d, err := strconv.Atoi(os.Getenv("REPLAY_RETENTION_DAYS")); err == nil {
		srv.ReplayRetentionDays = d
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	// The web app calls this API through its own same-origin proxy, so no
	// CORS headers are sent and the session cookie never leaves that origin.
	r.Use(auth.Middleware(accounts))

	handler := gen.HandlerFromMux(srv, r)

	addr := fmt.Sprintf(":%d", cfg.QueryAPIPort)
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		log.Printf("Query API listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}
