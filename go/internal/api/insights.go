package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/insights"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// InsightReader runs trend and trace stats queries.
type InsightReader interface {
	RunTrend(ctx context.Context, q ch.TrendQuery) ([]ch.TrendSeriesData, error)
}

func filters(in *[]gen.PropertyFilter) []ch.PropertyFilter {
	if in == nil {
		return nil
	}
	out := make([]ch.PropertyFilter, 0, len(*in))
	for _, f := range *in {
		pf := ch.PropertyFilter{Property: strings.TrimSpace(f.Property), Op: string(f.Op)}
		if f.Value != nil {
			pf.Value = *f.Value
		}
		out = append(out, pf)
	}
	return out
}

// trendQuery converts and validates a trend request. It returns the parsed
// formula, if any.
func (s *Server) trendQuery(project uint64, in gen.TrendQuery) (ch.TrendQuery, *insights.Formula, error) {
	q := ch.TrendQuery{Project: project, Filters: filters(in.Filters)}
	for _, si := range in.Series {
		ts := ch.TrendSeries{Filters: filters(si.Filters)}
		if si.Event != nil {
			ts.Event = strings.TrimSpace(*si.Event)
		}
		if si.Math != nil {
			ts.Math = string(*si.Math)
		}
		if si.Property != nil {
			ts.Property = strings.TrimSpace(*si.Property)
		}
		q.Series = append(q.Series, ts)
	}
	if in.Breakdown != nil {
		q.Breakdown = strings.TrimSpace(*in.Breakdown)
	}
	if in.Interval != nil {
		q.Interval = string(*in.Interval)
	}
	q.To = s.now().UTC()
	if in.To != nil {
		q.To = in.To.UTC()
	}
	q.From = q.To.Add(-7 * 24 * time.Hour)
	if in.From != nil {
		q.From = in.From.UTC()
	}
	if err := q.Validate(); err != nil {
		return q, nil, err
	}
	var f *insights.Formula
	if in.Formula != nil && strings.TrimSpace(*in.Formula) != "" {
		var err error
		if f, err = insights.ParseFormula(*in.Formula, len(q.Series)); err != nil {
			return q, nil, err
		}
	}
	return q, f, nil
}

func (s *Server) QueryTrend(w http.ResponseWriter, r *http.Request, params gen.QueryTrendParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	var body gen.TrendQuery
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	q, f, err := s.trendQuery(p, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Insights == nil {
		writeErr(w, http.StatusServiceUnavailable, "insights are not configured")
		return
	}
	data, err := s.Insights.RunTrend(r.Context(), q)
	if err != nil {
		internal(w, err)
		return
	}
	res := insights.Build(q, data, f)
	out := gen.TrendResult{Interval: gen.TrendInterval(q.Interval), From: q.From, To: q.To, Buckets: res.Buckets, Series: make([]gen.TrendSeriesResult, 0, len(res.Series))}
	if out.Buckets == nil {
		out.Buckets = []time.Time{}
	}
	for _, s := range res.Series {
		out.Series = append(out.Series, gen.TrendSeriesResult{Label: s.Label, Index: s.Index, Breakdown: s.Breakdown, Data: s.Data, Total: s.Total})
	}
	writeJSON(w, http.StatusOK, out)
}

// CheckWidget validates a saved dashboard widget query by kind.
func (s *Server) CheckWidget(kind string, raw json.RawMessage) error {
	switch kind {
	case "trend":
		var q gen.TrendQuery
		if err := json.Unmarshal(raw, &q); err != nil {
			return errors.New("invalid trend query")
		}
		_, _, err := s.trendQuery(1, q)
		return err
	case "funnel":
		var q gen.FunnelQuery
		if err := json.Unmarshal(raw, &q); err != nil {
			return errors.New("invalid funnel query")
		}
		fq := ch.FunnelQuery{Steps: q.Steps, WindowSeconds: 3600, From: time.Unix(0, 0), To: time.Unix(1, 0)}
		if q.WindowSeconds != nil {
			fq.WindowSeconds = *q.WindowSeconds
		}
		return fq.Validate()
	case "metric":
		var q struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &q); err != nil || strings.TrimSpace(q.Name) == "" {
			return errors.New("a metric widget needs a metric name")
		}
	case "traces":
		var q struct {
			Service string `json:"service"`
		}
		if err := json.Unmarshal(raw, &q); err != nil {
			return errors.New("invalid traces query")
		}
	}
	return nil
}
