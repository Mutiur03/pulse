// The alerting worker evaluates alert rules on a schedule against ClickHouse,
// fires new-issue and regression rules from the issue.events topic, and sends
// notifications by email, webhook and Slack.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/mail"
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
	cfg.ServiceName = "alerting-worker"

	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Printf("Failed to initialize tracer: %v", err)
	} else {
		defer func() { _ = cleanup(context.Background()) }()
	}

	db, err := pgxpool.New(context.Background(), cfg.PostgresURL)
	if err != nil {
		log.Fatalf("Failed to configure Postgres: %v", err)
	}
	defer db.Close()
	chClient, err := ch.Connect(cfg.ClickHouse)
	if err != nil {
		log.Fatalf("Failed to create ClickHouse client: %v", err)
	}
	defer func() { _ = chClient.Close() }()
	redisOpts, err := cfg.RedisOptions()
	if err != nil {
		log.Fatalf("Invalid REDIS_URL: %v", err)
	}
	rdb := redis.NewClient(redisOpts)
	defer func() { _ = rdb.Close() }()

	appURL := cfg.AppURL
	// Webhook and Slack deliveries never reach internal addresses unless
	// allowlisted here (development only).
	outbound, err := safehttp.ParseAllowlist(cfg.OutboundAllowlist)
	if err != nil {
		log.Fatalf("OUTBOUND_ALLOWLIST: %v", err)
	}
	engine := &alerts.Engine{
		Store:   &alerts.Store{DB: db},
		Metrics: chClient,
		Sender: &alerts.Notifier{
			// Without SMTP_ADDR emails are written to the log.
			Mail:   mail.New(cfg.SMTP),
			AppURL: appURL,
			Policy: outbound,
		},
		Locks:  alerts.RedisLocker{Client: rdb},
		AppURL: appURL,
	}

	consumer, err := kafka.NewConsumer(cfg.KafkaBootstrapServers, cfg.Kafka, "alerting-worker", []string{processor.TopicIssueEvents})
	if err != nil {
		log.Fatalf("Failed to create Kafka consumer: %v", err)
	}
	defer consumer.Close()

	mux := http.NewServeMux()
	mux.Handle("/metrics", monitor.Handler())
	monitor.Service("alerting-worker")
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy", "service": "alerting-worker"})
	})
	server := &http.Server{Addr: ":8083", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Println("Alerting worker health endpoint on :8083")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		t := time.NewTicker(tick)
		defer t.Stop()
		for {
			if _, err := engine.Tick(ctx); err != nil && ctx.Err() == nil {
				log.Printf("alerts: tick: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	go func() {
		for ctx.Err() == nil {
			records, err := consumer.Poll(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("alerts: poll: %v", err)
					time.Sleep(time.Second)
				}
				continue
			}
			for _, rec := range records {
				var ev issues.Event
				if err := json.Unmarshal(rec.Value, &ev); err != nil {
					log.Printf("alerts: bad issue event: %v", err)
					continue
				}
				if err := engine.HandleIssueEvent(ctx, ev); err != nil {
					log.Printf("alerts: issue event %d: %v", ev.IssueID, err)
				}
			}
			if len(records) > 0 {
				if err := consumer.Commit(ctx); err != nil && ctx.Err() == nil {
					log.Printf("alerts: commit: %v", err)
				}
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Shutting down...")
	cancel()
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	if err := server.Shutdown(sctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
}
