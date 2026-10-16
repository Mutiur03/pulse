// Package httpapi serves the issue endpoints through the generated strict
// interface.
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/modules/issues/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/issues/domain"
	"github.com/Mutiur03/pulse/internal/modules/issues/service"
	"github.com/Mutiur03/pulse/internal/symbolicate"
)

// MaxBody caps request bodies.
const MaxBody = 1 << 16

// Handlers implements oapi.StrictServerInterface.
type Handlers struct {
	svc   *service.Service
	sym   *symbolicate.Symbolicator
	clock apikit.Clock
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the issue handlers. sym may be nil.
func New(svc *service.Service, sym *symbolicate.Symbolicator, clock apikit.Clock) *Handlers {
	return &Handlers{svc: svc, sym: sym, clock: clock}
}

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, nil, oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON")))
}

func mapErr(err error) error {
	if errors.Is(err, domain.ErrNotFound) {
		return apikit.Err(http.StatusNotFound, "issue not found")
	}
	return err
}

func canRead(ctx context.Context) func(int64) bool {
	return func(project int64) bool { return apikit.CanRead(ctx, project) }
}

func toAPI(i domain.Issue) gen.Issue {
	return gen.Issue{
		Id: i.ID, ProjectId: i.ProjectID, Fingerprint: i.Fingerprint, Title: i.Title, ExceptionType: i.ExceptionType,
		Culprit: i.Culprit, Platform: i.Platform, ServiceName: i.ServiceName, Status: gen.IssueStatus(i.Status),
		FirstSeen: i.FirstSeen, LastSeen: i.LastSeen, Count: i.Count, SampleTraceId: i.SampleTraceID,
		SampleSpanId: apikit.StrPtr(i.SampleSpanID), ResolvedAt: i.ResolvedAt, TimesRegressed: i.TimesRegressed,
	}
}

// ListIssues implements GET /v1/issues.
func (h *Handlers) ListIssues(ctx context.Context, req oapi.ListIssuesRequestObject) (oapi.ListIssuesResponseObject, error) {
	params := req.Params
	p, err := apikit.Project(ctx, params.ProjectId)
	if err != nil {
		return nil, err
	}
	f := domain.ListFilter{ProjectID: int64(p), Limit: 25}
	if params.Status != nil {
		if !domain.Status(*params.Status).Valid() {
			return nil, apikit.Err(http.StatusBadRequest, "invalid status")
		}
		f.Status = domain.Status(*params.Status)
	}
	if params.Service != nil {
		f.Service = *params.Service
	}
	if params.Query != nil {
		f.Query = *params.Query
	}
	if params.Sort != nil {
		f.Sort = string(*params.Sort)
	}
	if params.Limit != nil {
		if *params.Limit < 1 || *params.Limit > 100 {
			return nil, apikit.Err(http.StatusBadRequest, "limit must be 1-100")
		}
		f.Limit = *params.Limit
	}
	if params.Offset != nil {
		if *params.Offset < 0 {
			return nil, apikit.Err(http.StatusBadRequest, "offset must be >= 0")
		}
		f.Offset = *params.Offset
	}
	page, err := h.svc.List(ctx, f, h.clock.Now())
	if err != nil {
		return nil, err
	}
	out := gen.IssueList{Issues: make([]gen.Issue, 0, len(page.Issues)), Total: page.Total, Limit: f.Limit, Offset: f.Offset}
	for _, i := range page.Issues {
		a := toAPI(i)
		if t, ok := page.Trends[uint64(i.ID)]; ok {
			a.Trend = &t
		}
		out.Issues = append(out.Issues, a)
	}
	return oapi.ListIssues200JSONResponse(out), nil
}

// GetIssue implements GET /v1/issues/{id}.
func (h *Handlers) GetIssue(ctx context.Context, req oapi.GetIssueRequestObject) (oapi.GetIssueResponseObject, error) {
	iss, err := h.svc.Get(ctx, req.Id, canRead(ctx))
	if err != nil {
		return nil, mapErr(err)
	}
	params := req.Params
	limit := 20
	if params.EventsLimit != nil && *params.EventsLimit >= 1 && *params.EventsLimit <= 100 {
		limit = *params.EventsLimit
	}
	recs, err := h.svc.Recent(ctx, iss, limit)
	if err != nil {
		return nil, err
	}
	out := gen.IssueDetail{Issue: toAPI(iss), RecentEvents: make([]gen.ErrorOccurrence, 0, len(recs))}
	again := params.Symbolicate != nil && *params.Symbolicate
	for _, e := range recs {
		o := apikit.ErrorOccurrence(e)
		frames, mapped := h.stackFrames(ctx, iss.ProjectID, e, again)
		o.Frames, o.MappedFrames = toFrames(frames), &mapped
		out.RecentEvents = append(out.RecentEvents, o)
	}
	return oapi.GetIssue200JSONResponse(out), nil
}

// UpdateIssue implements PATCH /v1/issues/{id}.
func (h *Handlers) UpdateIssue(ctx context.Context, req oapi.UpdateIssueRequestObject) (oapi.UpdateIssueResponseObject, error) {
	st := domain.Status(req.Body.Status)
	if !st.Valid() {
		return nil, apikit.Err(http.StatusBadRequest, "status must be unresolved, resolved or ignored")
	}
	iss, err := h.svc.SetStatus(ctx, req.Id, st, canRead(ctx))
	if err != nil {
		return nil, mapErr(err)
	}
	return oapi.UpdateIssue200JSONResponse(toAPI(iss)), nil
}
