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

	"github.com/Mutiur03/pulse/internal/config"
	"github.com/Mutiur03/pulse/internal/httpx"
	"github.com/Mutiur03/pulse/internal/kafka"
	"github.com/Mutiur03/pulse/internal/logging"
	"github.com/Mutiur03/pulse/internal/modules/issues"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps"
	"github.com/Mutiur03/pulse/internal/monitor"
	"github.com/Mutiur03/pulse/internal/processor"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
	"github.com/Mutiur03/pulse/internal/telemetry"
	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	cfg := config.Load()
	logging.Setup("stream-processor", cfg.LogLevel)
	cfg.ServiceName = "stream-processor"
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		slog.Error("stream processor stopped", "err", err)
		os.Exit(1)
	}
}

// topics are the raw topics the ingest gateway writes.
var topics = []string{"raw.events", "raw.spans", "raw.errors", "raw.logs", "raw.metrics"}

// run connects to every dependency and processes batches until ctx ends.
func run(ctx context.Context, cfg *config.Config) error {
	_, cleanup, err := telemetry.InitTracer(cfg.ServiceName)
	if err != nil {
		slog.Error("failed to initialize tracer", "err", err)
	} else {
		defer func() { _ = cleanup(context.Background()) }()
	}

	chClient, err := ch.Connect(cfg.ClickHouse)
	if err != nil {
		return fmt.Errorf("create ClickHouse client: %w", err)
	}
	defer func() { _ = chClient.Close() }()

	issueStore, err := connectIssues(ctx, cfg.PostgresURL)
	if err != nil {
		return fmt.Errorf("connect to Postgres: %w", err)
	}
	defer issueStore.Close()

	// Source maps live in object storage; without it stacks stay minified.
	var sym *symbolicate.Symbolicator
	if obj := replay.ConnectArtifacts(cfg.S3, cfg.ArtifactBucket); obj != nil {
		sym = &symbolicate.Symbolicator{Artifacts: sourcemaps.NewStore(issueStore.Pool()), Objects: obj}
	}

	producer, err := kafka.NewProducer(cfg.KafkaBootstrapServers, cfg.Kafka)
	if err != nil {
		return fmt.Errorf("create Kafka producer: %w", err)
	}
	defer producer.Close()

	consumer, err := kafka.NewConsumer(cfg.KafkaBootstrapServers, cfg.Kafka, "stream-processor", topics)
	if err != nil {
		return fmt.Errorf("create Kafka consumer: %w", err)
	}
	defer consumer.Close()

	go serveHealth(":8082")
	monitor.Service("stream-processor")
	slog.Info("stream processor started")

	p := &pipeline{sink: chClient, issues: issueStore, publisher: producer, sym: sym}
	for ctx.Err() == nil {
		records, err := consumer.Poll(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Error("failed to poll", "err", err)
			time.Sleep(time.Second)
			continue
		}
		if len(records) == 0 {
			continue
		}
		b := splitRecords(records)
		if !p.store(ctx, b) {
			// Only reached on shutdown: leave offsets uncommitted so the
			// batch is redelivered on the next start.
			break
		}
		monitor.ProcessorLastBatch.SetToCurrentTime()
		for topic, ts := range b.newest {
			monitor.ProcessorDelay.WithLabelValues(topic).Set(time.Since(ts).Seconds())
		}
		if err := consumer.Commit(ctx); err != nil {
			slog.Error("failed to commit", "err", err)
		}
	}
	slog.Info("shutting down")
	return nil
}

func serveHealth(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", monitor.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "healthy", "service": "stream-processor"})
	})
	slog.Info("stream processor health endpoint listening", "addr", addr)
	hs := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := hs.ListenAndServe(); err != nil {
		slog.Error("health server error", "err", err)
	}
}

type row = map[string]interface{}

// batch is one poll's records decoded and grouped by topic.
type batch struct {
	events, spans, errs, logs, metrics []row
	// newest is the latest record time per topic, for the delay metric.
	newest map[string]time.Time
}

func splitRecords(records []*kgo.Record) batch {
	b := batch{newest: map[string]time.Time{}}
	for _, record := range records {
		monitor.ProcessorRecords.WithLabelValues(record.Topic).Inc()
		if record.Timestamp.After(b.newest[record.Topic]) {
			b.newest[record.Topic] = record.Timestamp
		}
		var data row
		if err := json.Unmarshal(record.Value, &data); err != nil {
			monitor.ProcessorDecodeErrors.WithLabelValues(record.Topic).Inc()
			slog.Error("failed to unmarshal record", "topic", record.Topic, "err", err)
			continue
		}
		switch record.Topic {
		case "raw.events":
			b.events = append(b.events, data)
		case "raw.spans":
			b.spans = append(b.spans, data)
		case "raw.errors":
			b.errs = append(b.errs, data)
		case "raw.logs":
			b.logs = append(b.logs, data)
		case "raw.metrics":
			b.metrics = append(b.metrics, data)
		}
	}
	return b
}

// sink is the ClickHouse writer the pipeline needs.
type sink interface {
	InsertEvents(ctx context.Context, events []row) error
	InsertAliases(ctx context.Context, aliases []ch.Alias) error
	InsertSpans(ctx context.Context, spans []row) error
	InsertErrors(ctx context.Context, rows []ch.ErrorRow) error
	InsertLogs(ctx context.Context, logs []row) error
	InsertMetrics(ctx context.Context, points []row) error
}

// pipeline writes batches, grouping errors into issues first.
type pipeline struct {
	sink      sink
	issues    processor.IssueRecorder
	publisher processor.Publisher
	sym       *symbolicate.Symbolicator
}

// store writes every non-empty part of the batch. Writes retry until they
// succeed, so false means ctx ended and the batch must not be committed.
func (p *pipeline) store(ctx context.Context, b batch) bool {
	ok := true
	if len(b.events) > 0 {
		ok = p.write(ctx, "events", len(b.events), func() error { return p.sink.InsertEvents(ctx, b.events) }) && ok
		if ok {
			if aliases := ch.AliasesFromEvents(b.events); len(aliases) > 0 {
				if err := retry(ctx, func() error { return p.sink.InsertAliases(ctx, aliases) }); err != nil {
					slog.Error("failed to insert identity links", "count", len(aliases), "err", err)
				}
			}
		}
	}
	if len(b.spans) > 0 {
		ok = p.write(ctx, "spans", len(b.spans), func() error { return p.sink.InsertSpans(ctx, b.spans) }) && ok
	}
	if len(b.errs) > 0 {
		var rows []ch.ErrorRow
		if err := retry(ctx, func() error {
			var gerr error
			rows, gerr = processor.GroupErrorsWith(ctx, b.errs, p.issues, p.publisher, p.sym)
			return gerr
		}); err != nil {
			slog.Error("failed to group errors", "err", err)
			return false
		}
		ok = p.write(ctx, "errors", len(rows), func() error { return p.sink.InsertErrors(ctx, rows) }) && ok
	}
	if len(b.logs) > 0 {
		ok = p.write(ctx, "logs", len(b.logs), func() error { return p.sink.InsertLogs(ctx, b.logs) }) && ok
	}
	if len(b.metrics) > 0 {
		ok = p.write(ctx, "metrics", len(b.metrics), func() error { return p.sink.InsertMetrics(ctx, b.metrics) }) && ok
	}
	return ok
}

// write stores one part and logs the outcome.
func (p *pipeline) write(ctx context.Context, kind string, n int, fn func() error) bool {
	if err := write(ctx, kind, n, fn); err != nil {
		slog.Error("insert failed", "kind", kind, "count", n, "err", err)
		return false
	}
	slog.Info("inserted", "kind", kind, "count", n)
	return true
}

// connectIssues retries while Postgres starts up.
func connectIssues(ctx context.Context, url string) (*issues.Store, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		s, err := issues.Connect(c, url)
		cancel()
		if err == nil {
			return s, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, lastErr
		case <-time.After(2 * time.Second):
		}
	}
	return nil, lastErr
}

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

// retry runs fn until it succeeds or ctx is cancelled, backing off up to 30s.
// Blocking here keeps offsets uncommitted, so no batch is skipped.
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
