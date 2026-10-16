package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/logquery"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// LogsReader serves the logs explorer.
type LogsReader interface {
	SearchLogs(ctx context.Context, f ch.LogFilter) ([]ch.LogRow, error)
	LogHistogram(ctx context.Context, f ch.LogFilter, bucket time.Duration) ([]ch.LogBucket, error)
	LogFacets(ctx context.Context, f ch.LogFilter) ([]ch.LogFacet, error)
}

func (s *Server) logsFilter(w http.ResponseWriter, r *http.Request, p *gen.ProjectID, q *string, from, to *time.Time) (ch.LogFilter, bool) {
	project, ok := project(w, r, p)
	if !ok {
		return ch.LogFilter{}, false
	}
	if s.Logs == nil {
		writeErr(w, http.StatusServiceUnavailable, "logs are not configured")
		return ch.LogFilter{}, false
	}
	f := ch.LogFilter{Project: project}
	if q != nil {
		parsed, err := logquery.Parse(strings.TrimSpace(*q))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return f, false
		}
		f.Query = parsed
	}
	f.From, f.To = s.timeRange(from, to)
	if !f.From.Before(f.To) {
		writeErr(w, http.StatusBadRequest, "from must be before to")
		return f, false
	}
	return f, true
}

func logOut(l ch.LogRow) gen.LogRecord {
	attrs := map[string]interface{}{}
	if l.Attributes != "" {
		_ = json.Unmarshal([]byte(l.Attributes), &attrs)
	}
	return gen.LogRecord{
		Timestamp: l.Timestamp, Severity: l.Severity, ServiceName: l.Service, Body: l.Body,
		TraceId: strPtr(l.TraceID), SpanId: strPtr(l.SpanID), Attributes: attrs,
	}
}

func (s *Server) SearchLogs(w http.ResponseWriter, r *http.Request, params gen.SearchLogsParams) {
	f, ok := s.logsFilter(w, r, params.ProjectId, params.Q, params.From, params.To)
	if !ok {
		return
	}
	f.Limit = limit(params.Limit, 100, 500)
	if params.Before != nil {
		f.Before = *params.Before
	}
	if params.After != nil {
		f.After = *params.After
	}
	rows, err := s.Logs.SearchLogs(r.Context(), f)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.LogList{Logs: make([]gen.LogRecord, 0, len(rows)), Query: f.Query.String()}
	for _, l := range rows {
		out.Logs = append(out.Logs, logOut(l))
	}
	if len(rows) == f.Limit && params.After == nil {
		next := rows[len(rows)-1].Timestamp
		out.NextBefore = &next
	}
	writeJSON(w, http.StatusOK, out)
}

// logInterval picks a bucket size giving at most about 120 bars.
func logInterval(span time.Duration) time.Duration {
	for _, d := range []time.Duration{
		10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute,
		30 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour,
	} {
		if span/d <= 120 {
			return d
		}
	}
	return 24 * time.Hour
}

// LogBuckets fills the histogram, including empty buckets, oldest first.
func LogBuckets(rows []ch.LogBucket, from, to time.Time, step time.Duration) []gen.LogBucket {
	start := from.Truncate(step)
	byStart := map[int64]map[string]int64{}
	for _, r := range rows {
		k := r.Start.Unix()
		if byStart[k] == nil {
			byStart[k] = map[string]int64{}
		}
		level := r.Severity
		if level == "" {
			level = "NONE"
		}
		byStart[k][level] += int64(r.Count)
	}
	var out []gen.LogBucket
	for t := start; t.Before(to); t = t.Add(step) {
		c := byStart[t.Unix()]
		if c == nil {
			c = map[string]int64{}
		}
		out = append(out, gen.LogBucket{Start: t, Counts: c})
	}
	return out
}

func (s *Server) LogStats(w http.ResponseWriter, r *http.Request, params gen.LogStatsParams) {
	f, ok := s.logsFilter(w, r, params.ProjectId, params.Q, params.From, params.To)
	if !ok {
		return
	}
	step := logInterval(f.To.Sub(f.From))
	hist, err := s.Logs.LogHistogram(r.Context(), f, step)
	if err != nil {
		internal(w, err)
		return
	}
	facets, err := s.Logs.LogFacets(r.Context(), f)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.LogStats{
		IntervalSeconds: int(step / time.Second), Buckets: LogBuckets(hist, f.From, f.To, step),
		Services: []gen.LogFacetValue{}, Levels: []gen.LogFacetValue{},
	}
	for _, fc := range facets {
		v := gen.LogFacetValue{Value: fc.Value, Count: int64(fc.Count)}
		if fc.Field == "service" {
			out.Services = append(out.Services, v)
		} else {
			out.Levels = append(out.Levels, v)
			out.Total += v.Count
		}
	}
	writeJSON(w, http.StatusOK, out)
}
