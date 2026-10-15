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

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/store/ch"
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

	consumer, err := kafka.NewConsumer(cfg.KafkaBootstrapServers, "stream-processor", []string{"raw.events", "raw.spans"})
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
		if err := http.ListenAndServe(":8082", mux); err != nil {
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
