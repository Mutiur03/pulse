package httpapi

import (
	"encoding/json"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/insights"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// BuildTraceStats fills empty buckets with zeros.
func BuildTraceStats(q ch.TraceStatsQuery, rows []ch.TraceStatsRow) gen.TraceStats {
	buckets := insights.Buckets(q.From, q.To, q.Interval)
	by := make(map[int64]ch.TraceStatsRow, len(rows))
	for _, r := range rows {
		by[insights.BucketStart(r.Bucket, q.Interval).Unix()] = r
	}
	out := gen.TraceStats{Interval: gen.TrendInterval(q.Interval), Points: make([]gen.TraceStatsPoint, 0, len(buckets))}
	if q.Service != "" {
		out.Service = &q.Service
	}
	for _, b := range buckets {
		pt := gen.TraceStatsPoint{T: b}
		if r, ok := by[b.Unix()]; ok {
			pt.Requests, pt.Errors = int64(r.Requests), int64(r.Errors)
			pt.P50Ms, pt.P95Ms = r.P50Ns/1e6, r.P95Ns/1e6
			if r.Requests > 0 {
				pt.ErrorRate = float64(r.Errors) / float64(r.Requests)
			}
		}
		out.Requests += pt.Requests
		out.Errors += pt.Errors
		out.Points = append(out.Points, pt)
	}
	return out
}

func logOut(l ch.LogRow) gen.LogRecord {
	attrs := map[string]interface{}{}
	if l.Attributes != "" {
		_ = json.Unmarshal([]byte(l.Attributes), &attrs)
	}
	return gen.LogRecord{
		Timestamp: l.Timestamp, Severity: l.Severity, ServiceName: l.Service, Body: l.Body,
		TraceId: apikit.StrPtr(l.TraceID), SpanId: apikit.StrPtr(l.SpanID), Attributes: attrs,
	}
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
