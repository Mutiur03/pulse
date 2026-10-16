// Package service implements the trace, log and metric queries.
package service

import (
	"context"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/telemetry/domain"
	"github.com/Mutiur03/pulse/internal/modules/telemetry/ports"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Service runs telemetry queries. Nil Stats, Logs or Metrics disable
// those queries.
type Service struct {
	traces  ports.Traces
	stats   ports.Stats
	logs    ports.Logs
	metrics ports.Metrics
}

// New returns a Service.
func New(traces ports.Traces, stats ports.Stats, logs ports.Logs, metrics ports.Metrics) *Service {
	return &Service{traces: traces, stats: stats, logs: logs, metrics: metrics}
}

// Trace is everything recorded for one trace.
type Trace struct {
	Spans  []ch.SpanRecord
	Errors []ch.ErrorRecord
	Events []ch.EventRecord
}

// Trace loads a trace; ErrTraceNotFound when nothing references it.
func (s *Service) Trace(ctx context.Context, project uint64, id string) (Trace, error) {
	var t Trace
	var err error
	if t.Spans, err = s.traces.SpansByTrace(ctx, project, id); err != nil {
		return t, err
	}
	if t.Errors, err = s.traces.ErrorsByTrace(ctx, project, id); err != nil {
		return t, err
	}
	if t.Events, err = s.traces.EventsByTrace(ctx, project, id); err != nil {
		return t, err
	}
	if len(t.Spans) == 0 && len(t.Errors) == 0 && len(t.Events) == 0 {
		return t, domain.ErrTraceNotFound
	}
	return t, nil
}

// ListTraces searches trace summaries.
func (s *Service) ListTraces(ctx context.Context, f ch.TraceFilter) ([]ch.TraceSummaryRow, error) {
	return s.traces.ListTraces(ctx, f)
}

// Services lists the services seen in the range.
func (s *Service) Services(ctx context.Context, project uint64, from, to time.Time) ([]string, error) {
	return s.traces.Services(ctx, project, from, to)
}

// TraceStats validates the interval and range like a trend query, then
// computes the stats.
func (s *Service) TraceStats(ctx context.Context, q ch.TraceStatsQuery) ([]ch.TraceStatsRow, error) {
	tq := ch.TrendQuery{Project: q.Project, Series: []ch.TrendSeries{{}}, Interval: q.Interval, From: q.From, To: q.To}
	if err := tq.Validate(); err != nil {
		return nil, InvalidError{err}
	}
	if s.stats == nil {
		return nil, domain.ErrStatsOff
	}
	return s.stats.TraceStats(ctx, q)
}

// InvalidError is a query that fails validation.
type InvalidError struct{ Err error }

func (e InvalidError) Error() string { return e.Err.Error() }

// LogsEnabled reports whether logs can be queried.
func (s *Service) LogsEnabled() bool { return s.logs != nil }

// SearchLogs returns matching log lines, newest first.
func (s *Service) SearchLogs(ctx context.Context, f ch.LogFilter) ([]ch.LogRow, error) {
	if s.logs == nil {
		return nil, domain.ErrLogsOff
	}
	return s.logs.SearchLogs(ctx, f)
}

// LogStats is the histogram and facets of matching logs.
type LogStats struct {
	Step      time.Duration
	Histogram []ch.LogBucket
	Facets    []ch.LogFacet
}

// LogStats computes the histogram and facets of matching logs.
func (s *Service) LogStats(ctx context.Context, f ch.LogFilter) (LogStats, error) {
	if s.logs == nil {
		return LogStats{}, domain.ErrLogsOff
	}
	st := LogStats{Step: domain.LogInterval(f.To.Sub(f.From))}
	var err error
	if st.Histogram, err = s.logs.LogHistogram(ctx, f, st.Step); err != nil {
		return st, err
	}
	st.Facets, err = s.logs.LogFacets(ctx, f)
	return st, err
}

// MetricNames lists metrics seen in the range; none when metrics are off.
func (s *Service) MetricNames(ctx context.Context, project uint64, from, to time.Time) ([]ch.MetricName, error) {
	if s.metrics == nil {
		return nil, nil
	}
	return s.metrics.MetricNames(ctx, project, from, to)
}

// QueryMetric runs q; ok is false when metrics are off.
func (s *Service) QueryMetric(ctx context.Context, q ch.MetricQuery) (res ch.MetricResult, ok bool, err error) {
	if s.metrics == nil {
		return res, false, nil
	}
	res, err = s.metrics.QueryMetric(ctx, q)
	return res, true, err
}
