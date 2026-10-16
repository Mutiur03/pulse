// Package httpapi serves the trace, log and metric endpoints through the
// generated strict interface.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/logquery"
	"github.com/Mutiur03/pulse/internal/modules/telemetry/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/telemetry/domain"
	"github.com/Mutiur03/pulse/internal/modules/telemetry/service"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Handlers implements oapi.StrictServerInterface.
type Handlers struct {
	svc   *service.Service
	clock apikit.Clock
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the telemetry handlers.
func New(svc *service.Service, clock apikit.Clock) *Handlers {
	return &Handlers{svc: svc, clock: clock}
}

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, nil, oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON")))
}

func mapErr(err error) error {
	var invalid service.InvalidError
	switch {
	case errors.Is(err, domain.ErrTraceNotFound):
		return apikit.Err(http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrLogsOff), errors.Is(err, domain.ErrStatsOff):
		return apikit.Err(http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, domain.ErrBadRange):
		return apikit.Err(http.StatusBadRequest, err.Error())
	case errors.As(err, &invalid):
		return apikit.Err(http.StatusBadRequest, invalid.Error())
	}
	return err
}

// GetTrace implements GET /v1/traces/{trace_id}.
func (h *Handlers) GetTrace(ctx context.Context, req oapi.GetTraceRequestObject) (oapi.GetTraceResponseObject, error) {
	id := strings.ToLower(req.TraceId)
	p, err := apikit.Project(ctx, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	t, err := h.svc.Trace(ctx, p, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return oapi.GetTrace200JSONResponse(BuildTrace(id, t.Spans, t.Errors, t.Events)), nil
}

// ListTraces implements GET /v1/traces.
func (h *Handlers) ListTraces(ctx context.Context, req oapi.ListTracesRequestObject) (oapi.ListTracesResponseObject, error) {
	params := req.Params
	p, err := apikit.Project(ctx, params.ProjectId)
	if err != nil {
		return nil, err
	}
	f := ch.TraceFilter{Project: p, Limit: apikit.Limit(params.Limit, 50, 200)}
	f.From, f.To = h.clock.TimeRange(params.From, params.To)
	if params.Service != nil {
		f.Service = *params.Service
	}
	if params.Status != nil {
		if *params.Status != gen.ListTracesParamsStatusOk && *params.Status != gen.ListTracesParamsStatusError {
			return nil, apikit.Err(http.StatusBadRequest, "status must be ok or error")
		}
		f.Status = string(*params.Status)
	}
	if params.MinDurationMs != nil && *params.MinDurationMs > 0 {
		f.MinDurationNs = uint64(*params.MinDurationMs * 1e6)
	}
	if params.MaxDurationMs != nil && *params.MaxDurationMs > 0 {
		f.MaxDurationNs = uint64(*params.MaxDurationMs * 1e6)
	}
	if params.Query != nil {
		f.NameQuery = strings.TrimSpace(*params.Query)
	}
	rows, err := h.svc.ListTraces(ctx, f)
	if err != nil {
		return nil, err
	}
	out := gen.TraceList{Traces: make([]gen.TraceSummary, 0, len(rows))}
	for _, t := range rows {
		ts := gen.TraceSummary{
			TraceId: t.TraceID, RootName: t.RootName, RootService: t.RootService, Services: t.Services,
			StartTime: t.StartTime, DurationNs: t.DurationNs, SpanCount: int(t.SpanCount),
			ErrorCount: int(t.ErrorCount), HasBrowser: t.Browser > 0,
		}
		if ts.Services == nil {
			ts.Services = []string{}
		}
		if t.HTTPStatus != 0 {
			st := int(t.HTTPStatus)
			ts.HttpStatus = &st
		}
		out.Traces = append(out.Traces, ts)
	}
	return oapi.ListTraces200JSONResponse(out), nil
}

// ListServices implements GET /v1/services.
func (h *Handlers) ListServices(ctx context.Context, req oapi.ListServicesRequestObject) (oapi.ListServicesResponseObject, error) {
	p, err := apikit.Project(ctx, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	from, to := h.clock.TimeRange(req.Params.From, req.Params.To)
	list, err := h.svc.Services(ctx, p, from, to)
	if err != nil {
		return nil, err
	}
	return oapi.ListServices200JSONResponse(list), nil
}

// TraceStats implements GET /v1/traces/stats.
func (h *Handlers) TraceStats(ctx context.Context, req oapi.TraceStatsRequestObject) (oapi.TraceStatsResponseObject, error) {
	params := req.Params
	p, err := apikit.Project(ctx, params.ProjectId)
	if err != nil {
		return nil, err
	}
	q := ch.TraceStatsQuery{Project: p, Interval: "hour"}
	q.From, q.To = h.clock.TimeRange(params.From, params.To)
	if params.Interval != nil {
		q.Interval = string(*params.Interval)
	}
	if params.Service != nil {
		q.Service = strings.TrimSpace(*params.Service)
	}
	rows, err := h.svc.TraceStats(ctx, q)
	if err != nil {
		return nil, mapErr(err)
	}
	return oapi.TraceStats200JSONResponse(BuildTraceStats(q, rows)), nil
}

// logsFilter checks access and parses the shared log query parameters.
func (h *Handlers) logsFilter(ctx context.Context, p *gen.ProjectID, q *string, from, to *time.Time) (ch.LogFilter, error) {
	project, err := apikit.Project(ctx, p)
	if err != nil {
		return ch.LogFilter{}, err
	}
	if !h.svc.LogsEnabled() {
		return ch.LogFilter{}, mapErr(domain.ErrLogsOff)
	}
	f := ch.LogFilter{Project: project}
	if q != nil {
		parsed, err := logquery.Parse(strings.TrimSpace(*q))
		if err != nil {
			return f, apikit.Err(http.StatusBadRequest, err.Error())
		}
		f.Query = parsed
	}
	f.From, f.To = h.clock.TimeRange(from, to)
	if !f.From.Before(f.To) {
		return f, mapErr(domain.ErrBadRange)
	}
	return f, nil
}

// SearchLogs implements GET /v1/logs.
func (h *Handlers) SearchLogs(ctx context.Context, req oapi.SearchLogsRequestObject) (oapi.SearchLogsResponseObject, error) {
	params := req.Params
	f, err := h.logsFilter(ctx, params.ProjectId, params.Q, params.From, params.To)
	if err != nil {
		return nil, err
	}
	f.Limit = apikit.Limit(params.Limit, 100, 500)
	if params.Before != nil {
		f.Before = *params.Before
	}
	if params.After != nil {
		f.After = *params.After
	}
	rows, err := h.svc.SearchLogs(ctx, f)
	if err != nil {
		return nil, mapErr(err)
	}
	out := gen.LogList{Logs: make([]gen.LogRecord, 0, len(rows)), Query: f.Query.String()}
	for _, l := range rows {
		out.Logs = append(out.Logs, logOut(l))
	}
	if len(rows) == f.Limit && params.After == nil {
		next := rows[len(rows)-1].Timestamp
		out.NextBefore = &next
	}
	return oapi.SearchLogs200JSONResponse(out), nil
}

// LogStats implements GET /v1/logs/stats.
func (h *Handlers) LogStats(ctx context.Context, req oapi.LogStatsRequestObject) (oapi.LogStatsResponseObject, error) {
	params := req.Params
	f, err := h.logsFilter(ctx, params.ProjectId, params.Q, params.From, params.To)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.LogStats(ctx, f)
	if err != nil {
		return nil, mapErr(err)
	}
	out := gen.LogStats{
		IntervalSeconds: int(st.Step / time.Second), Buckets: LogBuckets(st.Histogram, f.From, f.To, st.Step),
		Services: []gen.LogFacetValue{}, Levels: []gen.LogFacetValue{},
	}
	for _, fc := range st.Facets {
		v := gen.LogFacetValue{Value: fc.Value, Count: int64(fc.Count)}
		if fc.Field == "service" {
			out.Services = append(out.Services, v)
		} else {
			out.Levels = append(out.Levels, v)
			out.Total += v.Count
		}
	}
	return oapi.LogStats200JSONResponse(out), nil
}

// ListMetricNames implements GET /v1/metrics/names.
func (h *Handlers) ListMetricNames(ctx context.Context, req oapi.ListMetricNamesRequestObject) (oapi.ListMetricNamesResponseObject, error) {
	p, err := apikit.Project(ctx, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	from, to := h.clock.TimeRange(req.Params.From, req.Params.To)
	list, err := h.svc.MetricNames(ctx, p, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]gen.MetricName, 0, len(list))
	for _, m := range list {
		out = append(out, gen.MetricName{Name: m.Name, Type: m.Type, Unit: m.Unit, Services: m.Services, Points: int64(m.Points)})
	}
	return oapi.ListMetricNames200JSONResponse(out), nil
}

// QueryMetric implements GET /v1/metrics/query.
func (h *Handlers) QueryMetric(ctx context.Context, req oapi.QueryMetricRequestObject) (oapi.QueryMetricResponseObject, error) {
	params := req.Params
	p, err := apikit.Project(ctx, params.ProjectId)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(params.Name)
	if name == "" || len(name) > 255 {
		return nil, apikit.Err(http.StatusBadRequest, "name is required")
	}
	q := ch.MetricQuery{Project: p, Name: name}
	q.From, q.To = h.clock.TimeRange(params.From, params.To)
	if !q.To.After(q.From) {
		return nil, mapErr(domain.ErrBadRange)
	}
	q.Step = domain.MetricStep(q.From, q.To, params.Step)
	if params.Agg != nil {
		q.Agg = string(*params.Agg)
	}
	if !ch.ValidMetricAgg(q.Agg) {
		return nil, apikit.Err(http.StatusBadRequest, "unknown agg")
	}
	if params.Service != nil {
		q.Service = strings.TrimSpace(*params.Service)
	}
	if params.GroupBy != nil {
		q.GroupBy = strings.TrimSpace(*params.GroupBy)
	}
	out := gen.MetricResult{Name: name, Agg: q.Agg, Step: int(q.Step / time.Second), Series: []gen.MetricSeries{}}
	res, ok, err := h.svc.QueryMetric(ctx, q)
	if err != nil {
		return nil, err
	}
	if !ok {
		return oapi.QueryMetric200JSONResponse(out), nil
	}
	out.Type, out.Unit, out.Agg = res.Type, res.Unit, res.Agg
	for _, ser := range res.Series {
		pts := make([]gen.MetricPoint, len(ser.Points))
		for i, pt := range ser.Points {
			pts[i] = gen.MetricPoint{T: pt.Time, V: pt.Value}
		}
		out.Series = append(out.Series, gen.MetricSeries{Label: ser.Label, Points: pts})
	}
	return oapi.QueryMetric200JSONResponse(out), nil
}
