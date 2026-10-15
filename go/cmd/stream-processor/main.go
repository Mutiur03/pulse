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

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			records, err := consumer.Poll(ctx)
			if err != nil {
				log.Printf("Failed to poll: %v", err)
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

			if len(events) > 0 {
				log.Printf("Processing %d events", len(events))
				if err := chClient.InsertEvents(ctx, events); err != nil {
					log.Printf("Failed to insert events: %v", err)
				} else {
					log.Printf("Successfully inserted %d events", len(events))
				}
			}

			if len(spans) > 0 {
				log.Printf("Processing %d spans", len(spans))
				if err := chClient.InsertSpans(ctx, spans); err != nil {
					log.Printf("Failed to insert spans: %v", err)
				} else {
					log.Printf("Successfully inserted %d spans", len(spans))
				}
			}

			if err := consumer.Commit(ctx); err != nil {
				log.Printf("Failed to commit: %v", err)
			}
		}
	}
}
