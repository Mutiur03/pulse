package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/ingest"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/logging"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Mutiur03/pulse/internal/governance"
	"github.com/Mutiur03/pulse/internal/monitor"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	cfg := config.Load()
	logging.Setup("ingest-gateway", cfg.LogLevel)
	cfg.ServiceName = "ingest-gateway"

	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		slog.Error("failed to initialize tracer", "err", err)
	} else {
		defer func() {
			_ = cleanup(context.Background())
		}()
	}

	producer, err := kafka.NewProducer(cfg.KafkaBootstrapServers, cfg.Kafka)
	if err != nil {
		log.Fatalf("Failed to create Kafka producer: %v", err)
	}
	defer producer.Close()

	db, err := pgxpool.New(context.Background(), cfg.PostgresURL)
	if err != nil {
		log.Fatalf("Failed to configure Postgres: %v", err)
	}
	defer db.Close()
	redisOpts, err := cfg.RedisOptions()
	if err != nil {
		log.Fatalf("Invalid REDIS_URL: %v", err)
	}
	rdb := redis.NewClient(redisOpts)
	defer func() { _ = rdb.Close() }()
	auth := &ingest.KeyAuth{
		Resolver: &keys.Resolver{Redis: rdb, Source: &keys.Store{DB: db}},
		Limiter:  &keys.Limiter{Redis: rdb},
	}

	var opts []ingest.Option
	if objects := replay.Connect(cfg.S3); objects != nil {
		chClient, err := ch.Connect(cfg.ClickHouse)
		if err != nil {
			log.Fatalf("Failed to create ClickHouse client: %v", err)
		}
		defer func() { _ = chClient.Close() }()
		opts = append(opts, ingest.WithReplay(&replay.Writer{Objects: objects, Meta: chClient}))
	}
	// PII scrubbing with each project's rules (internal/governance).
	scrub := &governance.Cache{Source: &governance.Store{DB: db}}
	opts = append(opts, ingest.WithScrubbing(scrub))
	if h := config.String("COUNTRY_HEADER", ""); h != "" {
		opts = append(opts, ingest.WithCountryHeader(h))
	}
	// Metrics are served on their own port, not the public one.
	monitor.Service("ingest-gateway")
	monitor.Serve(config.String("METRICS_ADDR", ":9101"))
	addr := fmt.Sprintf(":%d", cfg.IngestGatewayPort)
	server := &http.Server{
		Addr:              addr,
		Handler:           monitor.Middleware("ingest-gateway", ingestRoute)(ingest.NewHandler(producer, auth, opts...)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("ingest gateway listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	grpcServer, lis := startGRPC(producer, auth, scrub)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("server shutdown error", "err", err)
	}
	if grpcServer != nil {
		// Finish in-flight exports, then force close what remains.
		done := make(chan struct{})
		go func() { grpcServer.GracefulStop(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			grpcServer.Stop()
		}
		_ = lis.Close()
	}
}

// startGRPC serves OTLP/gRPC on OTLP_GRPC_PORT (default 4317; "0" or "off"
// disables it), with TLS when OTLP_GRPC_TLS_CERT and OTLP_GRPC_TLS_KEY are
// set and a size limit from OTLP_GRPC_MAX_RECV_MIB.
func startGRPC(p ingest.Producer, auth ingest.Authenticator, scrub ingest.ScrubSource) (*grpc.Server, net.Listener) {
	port := config.String("OTLP_GRPC_PORT", "4317")
	if port == "0" || port == "off" {
		return nil, nil
	}
	var opts []grpc.ServerOption
	if mib := config.PositiveInt("OTLP_GRPC_MAX_RECV_MIB", 0); mib > 0 {
		opts = append(opts, grpc.MaxRecvMsgSize(mib<<20))
	}
	cert, key := config.String("OTLP_GRPC_TLS_CERT", ""), config.String("OTLP_GRPC_TLS_KEY", "")
	if cert != "" || key != "" {
		creds, err := credentials.NewServerTLSFromFile(cert, key)
		if err != nil {
			log.Fatalf("OTLP gRPC TLS: %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("OTLP gRPC listen: %v", err)
	}
	s := ingest.NewGRPCServer(p, auth, scrub, opts...)
	go func() {
		slog.Info("OTLP gRPC receiver listening", "port", port, "tls", cert != "")
		if err := s.Serve(lis); err != nil && err != grpc.ErrServerStopped {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()
	return s, lis
}

var ingestRoutes = map[string]bool{"/v1/events": true, "/v1/traces": true, "/v1/errors": true, "/v1/logs": true,
	"/v1/metrics": true, "/v1/replay": true, "/health": true}

// ingestRoute keeps the route label bounded.
func ingestRoute(r *http.Request) string {
	if ingestRoutes[r.URL.Path] {
		return r.URL.Path
	}
	return "other"
}
