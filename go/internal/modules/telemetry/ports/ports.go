// Package ports declares the ClickHouse readers the telemetry module needs.
package ports

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Traces reads spans and what is attached to them.
type Traces interface {
	SpansByTrace(ctx context.Context, project uint64, traceID string) ([]ch.SpanRecord, error)
	ErrorsByTrace(ctx context.Context, project uint64, traceID string) ([]ch.ErrorRecord, error)
	EventsByTrace(ctx context.Context, project uint64, traceID string) ([]ch.EventRecord, error)
	ListTraces(ctx context.Context, f ch.TraceFilter) ([]ch.TraceSummaryRow, error)
	Services(ctx context.Context, project uint64, from, to time.Time) ([]string, error)
}

// Stats computes request rate, errors and latency over time.
type Stats interface {
	TraceStats(ctx context.Context, q ch.TraceStatsQuery) ([]ch.TraceStatsRow, error)
}

// Logs serves the logs explorer.
type Logs interface {
	SearchLogs(ctx context.Context, f ch.LogFilter) ([]ch.LogRow, error)
	LogHistogram(ctx context.Context, f ch.LogFilter, bucket time.Duration) ([]ch.LogBucket, error)
	LogFacets(ctx context.Context, f ch.LogFilter) ([]ch.LogFacet, error)
}

// Metrics reads OTLP metrics.
type Metrics interface {
	MetricNames(ctx context.Context, project uint64, from, to time.Time) ([]ch.MetricName, error)
	QueryMetric(ctx context.Context, q ch.MetricQuery) (ch.MetricResult, error)
}
