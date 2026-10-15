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

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/ingest"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	cfg.ServiceName = "ingest-gateway"

	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Printf("Failed to initialize tracer: %v", err)
	} else {
		defer func() {
			_ = cleanup(context.Background())
		}()
	}

	producer, err := kafka.NewProducer(cfg.KafkaBootstrapServers)
	if err != nil {
		log.Fatalf("Failed to create Kafka producer: %v", err)
	}
	defer producer.Close()

	db, err := pgxpool.New(context.Background(), cfg.PostgresURL)
	if err != nil {
		log.Fatalf("Failed to configure Postgres: %v", err)
	}
	defer db.Close()
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	defer func() { _ = rdb.Close() }()
	auth := &ingest.KeyAuth{
		Resolver: &keys.Resolver{Redis: rdb, Source: &keys.Store{DB: db}},
		Limiter:  &keys.Limiter{Redis: rdb},
	}

	var opts []ingest.Option
	if objects := replay.Connect(); objects != nil {
		chClient, err := ch.NewClient(cfg.ClickHouseAddr)
		if err != nil {
			log.Fatalf("Failed to create ClickHouse client: %v", err)
		}
		defer func() { _ = chClient.Close() }()
		opts = append(opts, ingest.WithReplay(&replay.Writer{Objects: objects, Meta: chClient}))
	}
	addr := fmt.Sprintf(":%d", cfg.IngestGatewayPort)
	server := &http.Server{
		Addr:              addr,
		Handler:           ingest.NewHandler(producer, auth, opts...),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("Ingest gateway listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}
