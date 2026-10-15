package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mutiur03/pulse/internal/api"
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/store/ch"
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
		Keys: &keys.Store{DB: db, Redis: rdb},
		// Set COOKIE_SECURE=false only for plain-HTTP local development.
		SecureCookie: os.Getenv("COOKIE_SECURE") != "false",
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
