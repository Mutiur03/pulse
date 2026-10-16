// Package httpapi serves the analytics endpoints through the generated
// strict interface.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/insights"
	"github.com/Mutiur03/pulse/internal/modules/analytics/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/analytics/domain"
	"github.com/Mutiur03/pulse/internal/modules/analytics/service"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Body limits per request kind.
const (
	MaxQueryBody = 1 << 16
	MaxSmallBody = 1 << 14
)

// Handlers implements oapi.StrictServerInterface.
type Handlers struct {
	svc   *service.Service
	clock apikit.Clock
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the analytics handlers.
func New(svc *service.Service, clock apikit.Clock) *Handlers {
	return &Handlers{svc: svc, clock: clock}
}

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, nil, oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON")))
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, domain.ErrAnalyticsOff), errors.Is(err, domain.ErrInsightsOff):
		return apikit.Err(http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, domain.ErrPersonNotFound):
		return apikit.Err(http.StatusNotFound, err.Error())
	}
	return err
}

func (h *Handlers) peopleReady() error {
	if !h.svc.PeopleEnabled() {
		return mapErr(domain.ErrAnalyticsOff)
	}
	return nil
}

func trim(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// ListEvents implements GET /v1/events.
func (h *Handlers) ListEvents(ctx context.Context, req oapi.ListEventsRequestObject) (oapi.ListEventsResponseObject, error) {
	params := req.Params
	p, err := apikit.Project(ctx, params.ProjectId)
	if err != nil {
		return nil, err
	}
	f := ch.EventFilter{Project: p, Limit: apikit.Limit(params.Limit, 50, 200),
		Event: trim(params.Event), DistinctID: trim(params.DistinctId), SessionID: trim(params.SessionId)}
	f.From, f.To = h.clock.TimeRange(params.From, params.To)
	rows, err := h.svc.Events(ctx, f)
	if err != nil {
		return nil, err
	}
	out := gen.EventList{Events: make([]gen.ProductEvent, 0, len(rows))}
	for _, e := range rows {
		pe := apikit.ProductEvent(e.EventRecord)
		pe.TraceId = apikit.StrPtr(e.TraceID)
		out.Events = append(out.Events, pe)
	}
	return oapi.ListEvents200JSONResponse(out), nil
}

// ListEventNames implements GET /v1/events/names.
func (h *Handlers) ListEventNames(ctx context.Context, req oapi.ListEventNamesRequestObject) (oapi.ListEventNamesResponseObject, error) {
	p, err := apikit.Project(ctx, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	from, to := h.clock.TimeRange(req.Params.From, req.Params.To)
	rows, err := h.svc.EventNames(ctx, p, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]gen.EventName, 0, len(rows))
	for _, n := range rows {
		out = append(out, gen.EventName{Name: n.Name, Count: int64(n.Count)})
	}
	return oapi.ListEventNames200JSONResponse(out), nil
}

// QueryFunnel implements POST /v1/funnels/query.
func (h *Handlers) QueryFunnel(ctx context.Context, req oapi.QueryFunnelRequestObject) (oapi.QueryFunnelResponseObject, error) {
	p, err := apikit.Project(ctx, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	body := req.Body
	q := ch.FunnelQuery{Project: p, Steps: body.Steps, WindowSeconds: domain.FunnelSeconds}
	if body.WindowSeconds != nil {
		q.WindowSeconds = *body.WindowSeconds
	}
	q.From, q.To = domain.Range(h.clock.Now().UTC(), body.From, body.To, domain.FunnelWindow)
	if err := q.Validate(); err != nil {
		return nil, apikit.BadRequest(err)
	}
	d, err := h.svc.Funnel(ctx, q)
	if err != nil {
		return nil, err
	}
	return oapi.QueryFunnel200JSONResponse(BuildFunnel(q, d)), nil
}

// QueryTrend implements POST /v1/insights/trends.
func (h *Handlers) QueryTrend(ctx context.Context, req oapi.QueryTrendRequestObject) (oapi.QueryTrendResponseObject, error) {
	p, err := apikit.Project(ctx, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	q, f, err := TrendQuery(h.clock.Now(), p, *req.Body)
	if err != nil {
		return nil, apikit.BadRequest(err)
	}
	data, err := h.svc.Trend(ctx, q)
	if err != nil {
		return nil, mapErr(err)
	}
	res := insights.Build(q, data, f)
	out := gen.TrendResult{Interval: gen.TrendInterval(q.Interval), From: q.From, To: q.To, Buckets: res.Buckets, Series: make([]gen.TrendSeriesResult, 0, len(res.Series))}
	if out.Buckets == nil {
		out.Buckets = []time.Time{}
	}
	for _, s := range res.Series {
		out.Series = append(out.Series, gen.TrendSeriesResult{Label: s.Label, Index: s.Index, Breakdown: s.Breakdown, Data: s.Data, Total: s.Total})
	}
	return oapi.QueryTrend200JSONResponse(out), nil
}

// ListPersons implements GET /v1/persons.
func (h *Handlers) ListPersons(ctx context.Context, req oapi.ListPersonsRequestObject) (oapi.ListPersonsResponseObject, error) {
	params := req.Params
	p, err := apikit.Project(ctx, params.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := h.peopleReady(); err != nil {
		return nil, err
	}
	f := ch.PersonFilter{Project: p, Limit: apikit.Limit(params.Limit, 50, 200), Search: trim(params.Search)}
	f.From, f.To = domain.Range(h.clock.Now().UTC(), params.From, params.To, domain.PersonWindow)
	if len(f.Search) > 200 {
		return nil, apikit.Err(http.StatusBadRequest, "search is too long")
	}
	rows, err := h.svc.Persons(ctx, f)
	if err != nil {
		return nil, mapErr(err)
	}
	out := gen.PersonList{Persons: make([]gen.Person, 0, len(rows))}
	for _, row := range rows {
		out.Persons = append(out.Persons, personOut(row))
	}
	return oapi.ListPersons200JSONResponse(out), nil
}

// GetPerson implements GET /v1/persons/profile.
func (h *Handlers) GetPerson(ctx context.Context, req oapi.GetPersonRequestObject) (oapi.GetPersonResponseObject, error) {
	params := req.Params
	p, err := apikit.Project(ctx, params.ProjectId)
	if err != nil {
		return nil, err
	}
	if err := h.peopleReady(); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(params.Id)
	if id == "" || len(id) > 200 {
		return nil, apikit.Err(http.StatusBadRequest, "id is required")
	}
	from, to := domain.Range(h.clock.Now().UTC(), params.From, params.To, domain.PersonWindow)
	d, err := h.svc.Person(ctx, p, id, from, to)
	if err != nil {
		return nil, mapErr(err)
	}
	out := gen.PersonDetail{Person: personOut(d.Person), Sessions: make([]gen.PersonSession, 0, len(d.Sessions)), Events: make([]gen.ProductEvent, 0, len(d.Events))}
	for _, ps := range d.Sessions {
		out.Sessions = append(out.Sessions, gen.PersonSession{
			SessionId: ps.SessionID, Start: ps.Start, End: ps.End, Events: int64(ps.Events), Pageviews: int64(ps.Pageviews),
			EntryPath: ps.EntryPath, ExitPath: ps.ExitPath, HasReplay: ps.Replay == 1, Errors: int64(ps.Errors),
		})
	}
	for _, e := range d.Events {
		out.Events = append(out.Events, apikit.ProductEvent(e))
	}
	return oapi.GetPerson200JSONResponse(out), nil
}

// QueryRetention implements POST /v1/insights/retention.
func (h *Handlers) QueryRetention(ctx context.Context, req oapi.QueryRetentionRequestObject) (oapi.QueryRetentionResponseObject, error) {
	p, err := apikit.Project(ctx, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	body := req.Body
	q := ch.RetentionQuery{Project: p, StartEvent: trim(body.StartEvent), ReturnEvent: trim(body.ReturnEvent)}
	if body.Period != nil {
		q.Period = string(*body.Period)
	}
	if body.Periods != nil {
		q.Periods = *body.Periods
	}
	now := h.clock.Now().UTC()
	to := body.To
	if to != nil {
		t := to.UTC()
		to = &t
	}
	from := body.From
	if from != nil {
		f := from.UTC()
		from = &f
	}
	q.From, q.To = domain.Range(now, from, to, domain.RetentionWindow(q.Period))
	if err := q.Validate(); err != nil {
		return nil, apikit.BadRequest(err)
	}
	d, err := h.svc.Retention(ctx, q)
	if err != nil {
		return nil, mapErr(err)
	}
	out := gen.RetentionResult{Period: gen.RetentionResultPeriod(q.Period), Periods: q.Periods, Cohorts: []gen.RetentionCohort{}}
	for _, c := range insights.BuildRetention(q, d, now) {
		out.Cohorts = append(out.Cohorts, gen.RetentionCohort{Start: c.Start, Size: c.Size, Values: c.Values})
	}
	return oapi.QueryRetention200JSONResponse(out), nil
}

// QueryPaths implements POST /v1/insights/paths.
func (h *Handlers) QueryPaths(ctx context.Context, req oapi.QueryPathsRequestObject) (oapi.QueryPathsResponseObject, error) {
	p, err := apikit.Project(ctx, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	body := req.Body
	q := ch.PathsQuery{Project: p, Start: trim(body.Start)}
	if body.Mode != nil {
		q.Mode = string(*body.Mode)
	}
	if body.Steps != nil {
		q.Steps = *body.Steps
	}
	q.To = h.clock.Now().UTC()
	if body.To != nil {
		q.To = body.To.UTC()
	}
	q.From = q.To.Add(-domain.PathsWindow)
	if body.From != nil {
		q.From = body.From.UTC()
	}
	if err := q.Validate(); err != nil {
		return nil, apikit.BadRequest(err)
	}
	rows, err := h.svc.Paths(ctx, q)
	if err != nil {
		return nil, mapErr(err)
	}
	res := insights.BuildPaths(rows, q.Steps, 10)
	out := gen.PathsResult{Mode: q.Mode, Steps: q.Steps, TotalSessions: res.Total, Nodes: []gen.PathNode{}, Links: []gen.PathLink{}, TopPaths: []gen.PathCount{}}
	for _, n := range res.Nodes {
		out.Nodes = append(out.Nodes, gen.PathNode{Step: n.Step, Name: n.Name, Sessions: n.Sessions, Dropoff: n.Dropoff})
	}
	for _, l := range res.Links {
		out.Links = append(out.Links, gen.PathLink{Step: l.Step, Source: l.Source, Target: l.Target, Sessions: l.Sessions})
	}
	for _, t := range res.Top {
		out.TopPaths = append(out.TopPaths, gen.PathCount{Path: t.Path, Sessions: int64(t.Sessions)})
	}
	return oapi.QueryPaths200JSONResponse(out), nil
}
