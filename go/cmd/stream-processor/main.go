package main

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mutiur03/pulse/internal/artifacts"
	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/logging"
	"github.com/Mutiur03/pulse/internal/monitor"
	"github.com/Mutiur03/pulse/internal/processor"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
	"github.com/Mutiur03/pulse/internal/telemetry"
)

func main() {
	cfg := config.Load()
	logging.Setup("stream-processor", cfg.LogLevel)
	cfg.ServiceName = "stream-processor"

	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		slog.Error("failed to initialize tracer", "err", err)
	} else {
		defer func() {
			_ = cleanup(context.Background())
		}()
	}

	chClient, err := ch.Connect(cfg.ClickHouse)
	if err != nil {
		log.Fatalf("Failed to create ClickHouse client: %v", err)
	}
	defer func() {
		_ = chClient.Close()
	}()

	issueStore, err := connectIssues(cfg.PostgresURL)
	if err != nil {
		log.Fatalf("Failed to connect to Postgres: %v", err)
	}
	defer issueStore.Close()

	// Source maps live in object storage; without it stacks stay minified.
	var sym *symbolicate.Symbolicator
	if obj := replay.ConnectArtifacts(cfg.S3, cfg.ArtifactBucket); obj != nil {
		sym = &symbolicate.Symbolicator{Artifacts: &artifacts.Store{DB: issueStore.Pool()}, Objects: obj}
	}

	producer, err := kafka.NewProducer(cfg.KafkaBootstrapServers, cfg.Kafka)
	if err != nil {
		log.Fatalf("Failed to create Kafka producer: %v", err)
	}
	defer producer.Close()

	consumer, err := kafka.NewConsumer(cfg.KafkaBootstrapServers, cfg.Kafka, "stream-processor", []string{"raw.events", "raw.spans", "raw.errors", "raw.logs", "raw.metrics"})
	if err != nil {
		log.Fatalf("Failed to create Kafka consumer: %v", err)
	}
	defer consumer.Close()

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", monitor.Handler())
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"healthy","service":"stream-processor"}`))
		})
		slog.Info("stream processor health endpoint listening", "addr", ":8082")
		hs := &http.Server{Addr: ":8082", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		if err := hs.ListenAndServe(); err != nil {
			slog.Error("health server error", "err", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("shutting down")
		cancel()
	}()

	monitor.Service("stream-processor")
	slog.Info("stream processor started")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			records, err := consumer.Poll(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Error("failed to poll", "err", err)
				time.Sleep(time.Second)
				continue
			}

			if len(records) == 0 {
				continue
			}

			var events []map[string]interface{}
			var spans []map[string]interface{}
			var errs []map[string]interface{}
			var logs []map[string]interface{}
			var metrics []map[string]interface{}

			newest := map[string]time.Time{}
			for _, record := range records {
				monitor.ProcessorRecords.WithLabelValues(record.Topic).Inc()
				if record.Timestamp.After(newest[record.Topic]) {
					newest[record.Topic] = record.Timestamp
				}
				var data map[string]interface{}
				if err := json.Unmarshal(record.Value, &data); err != nil {
					monitor.ProcessorDecodeErrors.WithLabelValues(record.Topic).Inc()
					slog.Error("failed to unmarshal record", "err", err)
					continue
				}

				if record.Topic == "raw.events" {
					events = append(events, data)
				} else if record.Topic == "raw.spans" {
					spans = append(spans, data)
				} else if record.Topic == "raw.errors" {
					errs = append(errs, data)
				} else if record.Topic == "raw.logs" {
					logs = append(logs, data)
				} else if record.Topic == "raw.metrics" {
					metrics = append(metrics, data)
				}
			}

			ok := true
			if len(events) > 0 {
				slog.Info("processing events", "count", len(events))
				if err := write(ctx, "events", len(events), func() error { return chClient.InsertEvents(ctx, events) }); err != nil {
					slog.Error("failed to insert events", "err", err)
					ok = false
				} else {
					slog.Info("successfully inserted events", "count", len(events))
					if aliases := ch.AliasesFromEvents(events); len(aliases) > 0 {
						if err := retry(ctx, func() error { return chClient.InsertAliases(ctx, aliases) }); err != nil {
							slog.Error("failed to insert identity links", "count", len(aliases), "err", err)
						}
					}
				}
			}

			if len(spans) > 0 {
				slog.Info("processing spans", "count", len(spans))
				if err := write(ctx, "spans", len(spans), func() error { return chClient.InsertSpans(ctx, spans) }); err != nil {
					slog.Error("failed to insert spans", "err", err)
					ok = false
				} else {
					slog.Info("successfully inserted spans", "count", len(spans))
				}
			}

			if len(errs) > 0 {
				var rows []ch.ErrorRow
				if err := retry(ctx, func() error {
					var gerr error
					rows, gerr = processor.GroupErrorsWith(ctx, errs, issueStore, producer, sym)
					return gerr
				}); err != nil {
					slog.Error("failed to group errors", "err", err)
					return
				}
				if err := write(ctx, "errors", len(rows), func() error { return chClient.InsertErrors(ctx, rows) }); err != nil {
					slog.Error("failed to insert errors", "err", err)
					ok = false
				} else {
					slog.Info("successfully inserted errors", "count", len(errs))
				}
			}

			if len(logs) > 0 {
				if err := write(ctx, "logs", len(logs), func() error { return chClient.InsertLogs(ctx, logs) }); err != nil {
					slog.Error("failed to insert logs", "err", err)
					ok = false
				} else {
					slog.Info("successfully inserted logs", "count", len(logs))
				}
			}

			if len(metrics) > 0 {
				if err := write(ctx, "metrics", len(metrics), func() error { return chClient.InsertMetrics(ctx, metrics) }); err != nil {
					slog.Error("failed to insert metrics", "err", err)
					ok = false
				} else {
					slog.Info("successfully inserted metric points", "count", len(metrics))
				}
			}

			if !ok {
				// Only reached on shutdown: leave offsets uncommitted so the
				// batch is redelivered on the next start.
				return
			}
			monitor.ProcessorLastBatch.SetToCurrentTime()
			for topic, ts := range newest {
				monitor.ProcessorDelay.WithLabelValues(topic).Set(time.Since(ts).Seconds())
			}
			if err := consumer.Commit(ctx); err != nil {
				slog.Error("failed to commit", "err", err)
			}
		}
	}
}

// connectIssues retries while Postgres starts up.
func connectIssues(url string) (*issues.Store, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		s, err := issues.Connect(ctx, url)
		cancel()
		if err == nil {
			return s, nil
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}
	return nil, lastErr
}

// retry runs fn until it succeeds or ctx is cancelled, backing off up to 30s.
// Blocking here keeps offsets uncommitted, so no batch is skipped.
// write stores one batch with retries and records its metrics.
func write(ctx context.Context, kind string, n int, fn func() error) error {
	start := time.Now()
	err := retry(ctx, fn)
	monitor.ProcessorWriteLatency.WithLabelValues(kind).Observe(time.Since(start).Seconds())
	if err != nil {
		monitor.ProcessorWriteErrors.WithLabelValues(kind).Inc()
		return err
	}
	monitor.ProcessorRowsWritten.WithLabelValues(kind).Add(float64(n))
	return nil
}

func retry(ctx context.Context, fn func() error) error {
	backoff := time.Second
	for {
		err := fn()
		if err == nil || ctx.Err() != nil {
			return err
		}
		slog.Warn("ClickHouse write failed, retrying", "backoff", backoff, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
