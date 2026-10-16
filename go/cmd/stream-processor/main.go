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

	"github.com/Mutiur03/pulse/internal/artifacts"
	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/processor"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
	"github.com/Mutiur03/pulse/internal/telemetry"
)

func main() {
	cfg := config.Load()
	cfg.ServiceName = "stream-processor"

	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		log.Printf("Failed to initialize tracer: %v", err)
	} else {
		defer func() {
			_ = cleanup(context.Background())
		}()
	}

	chClient, err := ch.NewClient(cfg.ClickHouseAddr)
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
	if obj := replay.ConnectArtifacts(); obj != nil {
		sym = &symbolicate.Symbolicator{Artifacts: &artifacts.Store{DB: issueStore.Pool()}, Objects: obj}
	}

	producer, err := kafka.NewProducer(cfg.KafkaBootstrapServers)
	if err != nil {
		log.Fatalf("Failed to create Kafka producer: %v", err)
	}
	defer producer.Close()

	consumer, err := kafka.NewConsumer(cfg.KafkaBootstrapServers, "stream-processor", []string{"raw.events", "raw.spans", "raw.errors", "raw.logs", "raw.metrics"})
	if err != nil {
		log.Fatalf("Failed to create Kafka consumer: %v", err)
	}
	defer consumer.Close()

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"healthy","service":"stream-processor"}`))
		})
		log.Println("Stream processor health endpoint on :8082")
		hs := &http.Server{Addr: ":8082", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		if err := hs.ListenAndServe(); err != nil {
			log.Printf("Health server error: %v", err)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel()
	}()

	log.Println("Stream processor started")

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
				log.Printf("Failed to poll: %v", err)
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

			for _, record := range records {
				var data map[string]interface{}
				if err := json.Unmarshal(record.Value, &data); err != nil {
					log.Printf("Failed to unmarshal record: %v", err)
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
				log.Printf("Processing %d events", len(events))
				if err := retry(ctx, func() error { return chClient.InsertEvents(ctx, events) }); err != nil {
					log.Printf("Failed to insert events: %v", err)
					ok = false
				} else {
					log.Printf("Successfully inserted %d events", len(events))
					if aliases := ch.AliasesFromEvents(events); len(aliases) > 0 {
						if err := retry(ctx, func() error { return chClient.InsertAliases(ctx, aliases) }); err != nil {
							log.Printf("Failed to insert %d identity links: %v", len(aliases), err)
						}
					}
				}
			}

			if len(spans) > 0 {
				log.Printf("Processing %d spans", len(spans))
				if err := retry(ctx, func() error { return chClient.InsertSpans(ctx, spans) }); err != nil {
					log.Printf("Failed to insert spans: %v", err)
					ok = false
				} else {
					log.Printf("Successfully inserted %d spans", len(spans))
				}
			}

			if len(errs) > 0 {
				var rows []ch.ErrorRow
				if err := retry(ctx, func() error {
					var gerr error
					rows, gerr = processor.GroupErrorsWith(ctx, errs, issueStore, producer, sym)
					return gerr
				}); err != nil {
					log.Printf("Failed to group errors: %v", err)
					return
				}
				if err := retry(ctx, func() error { return chClient.InsertErrors(ctx, rows) }); err != nil {
					log.Printf("Failed to insert errors: %v", err)
					ok = false
				} else {
					log.Printf("Successfully inserted %d errors", len(errs))
				}
			}

			if len(logs) > 0 {
				if err := retry(ctx, func() error { return chClient.InsertLogs(ctx, logs) }); err != nil {
					log.Printf("Failed to insert logs: %v", err)
					ok = false
				} else {
					log.Printf("Successfully inserted %d logs", len(logs))
				}
			}

			if len(metrics) > 0 {
				if err := retry(ctx, func() error { return chClient.InsertMetrics(ctx, metrics) }); err != nil {
					log.Printf("Failed to insert metrics: %v", err)
					ok = false
				} else {
					log.Printf("Successfully inserted %d metric points", len(metrics))
				}
			}

			if !ok {
				// Only reached on shutdown: leave offsets uncommitted so the
				// batch is redelivered on the next start.
				return
			}
			if err := consumer.Commit(ctx); err != nil {
				log.Printf("Failed to commit: %v", err)
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
func retry(ctx context.Context, fn func() error) error {
	backoff := time.Second
	for {
		err := fn()
		if err == nil || ctx.Err() != nil {
			return err
		}
		log.Printf("ClickHouse write failed, retrying in %s: %v", backoff, err)
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
