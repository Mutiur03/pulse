// The alerting worker evaluates alert rules on a schedule against ClickHouse,
// fires new-issue and regression rules from the issue.events topic, and sends
// notifications by email, webhook and Slack.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/logging"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/modules/issues"
	"github.com/Mutiur03/pulse/internal/monitor"
	"github.com/Mutiur03/pulse/internal/processor"
	"github.com/Mutiur03/pulse/internal/safehttp"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/telemetry"
)

// tick is how often due rules are looked for; each rule still runs on its
// own interval.
const tick = 5 * time.Second

func main() {
	cfg := config.Load()
	logging.Setup("alerting-worker", cfg.LogLevel)
	cfg.ServiceName = "alerting-worker"
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		slog.Error("alerting worker stopped", "err", err)
		os.Exit(1)
	}
}

// run wires the alert engine and serves until ctx ends or the health
// server fails.
func run(ctx context.Context, cfg *config.Config) error {
	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		slog.Error("failed to initialize tracer", "err", err)
	} else {
		defer func() { _ = cleanup(context.Background()) }()
	}

	// Webhook and Slack deliveries never reach internal addresses unless
	// allowlisted here (development only).
	outbound, err := safehttp.ParseAllowlist(cfg.OutboundAllowlist)
	if err != nil {
		return fmt.Errorf("OUTBOUND_ALLOWLIST: %w", err)
	}
	redisOpts, err := cfg.RedisOptions()
	if err != nil {
		return fmt.Errorf("invalid REDIS_URL: %w", err)
	}
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
	rdb := redis.NewClient(redisOpts)
	defer func() { _ = rdb.Close() }()

	engine := &alerts.Engine{
		Store:   &alerts.Store{DB: db},
		Metrics: chClient,
		Sender: &alerts.Notifier{
			// Without SMTP_ADDR emails are written to the log.
			Mail:   mail.New(cfg.SMTP),
			AppURL: cfg.AppURL,
			Policy: outbound,
		},
		Locks:  alerts.RedisLocker{Client: rdb},
		AppURL: cfg.AppURL,
	}

	consumer, err := kafka.NewConsumer(cfg.KafkaBootstrapServers, cfg.Kafka, "alerting-worker", []string{processor.TopicIssueEvents})
	if err != nil {
		return fmt.Errorf("create Kafka consumer: %w", err)
	}
	defer consumer.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go tickLoop(ctx, engine)
	go consumeIssueEvents(ctx, consumer, engine)
	return serveHealth(ctx, ":8083")
}

// serveHealth serves /health and /metrics until ctx ends.
func serveHealth(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", monitor.Handler())
	monitor.Service("alerting-worker")
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy", "service": "alerting-worker"})
	})
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() {
		slog.Info("alerting worker health endpoint listening", "addr", addr)
		errc <- server.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return fmt.Errorf("health server: %w", err)
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	if err := server.Shutdown(sctx); err != nil {
		slog.Error("server shutdown error", "err", err)
	}
	return nil
}

// tickLoop runs due alert rules every tick until ctx ends.
func tickLoop(ctx context.Context, engine *alerts.Engine) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		if _, err := engine.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("alerts: tick", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// issueSource is the part of the Kafka consumer the event loop uses.
type issueSource interface {
	Poll(ctx context.Context) ([]*kgo.Record, error)
	Commit(ctx context.Context) error
}

// issueHandler reacts to one issue lifecycle event.
type issueHandler interface {
	HandleIssueEvent(ctx context.Context, ev issues.Event) error
}

// consumeIssueEvents feeds issue.events to the engine until ctx ends.
func consumeIssueEvents(ctx context.Context, src issueSource, h issueHandler) {
	for ctx.Err() == nil {
		records, err := src.Poll(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("alerts: poll", "err", err)
				time.Sleep(time.Second)
			}
			continue
		}
		handleRecords(ctx, records, h)
		if len(records) > 0 {
			if err := src.Commit(ctx); err != nil && ctx.Err() == nil {
				slog.Error("alerts: commit", "err", err)
			}
		}
	}
}

func handleRecords(ctx context.Context, records []*kgo.Record, h issueHandler) {
	for _, rec := range records {
		var ev issues.Event
		if err := json.Unmarshal(rec.Value, &ev); err != nil {
			slog.Error("alerts: bad issue event", "err", err)
			continue
		}
		if err := h.HandleIssueEvent(ctx, ev); err != nil {
			slog.Error("alerts: issue event", "issue_id", ev.IssueID, "err", err)
		}
	}
}
