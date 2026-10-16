// Package httpapi serves the replay endpoints through the generated strict
// interface.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/modules/replay/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/replay/domain"
	"github.com/Mutiur03/pulse/internal/modules/replay/service"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Handlers implements oapi.StrictServerInterface.
type Handlers struct {
	svc   *service.Service
	clock apikit.Clock
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the replay handlers.
func New(svc *service.Service, clock apikit.Clock) *Handlers {
	return &Handlers{svc: svc, clock: clock}
}

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	return oapi.NewStrictHandlerWithOptions(h, nil, oapi.StrictHTTPServerOptions(apikit.Strict("invalid request body")))
}

func replaySummary(r ch.ReplaySummary) gen.ReplaySummary {
	urls := r.URLs
	if urls == nil {
		urls = []string{}
	}
	return gen.ReplaySummary{SessionId: r.SessionID, DistinctId: r.DistinctID, Start: r.Start, End: r.End,
		DurationMs: r.DurationMs, PageCount: int64(r.Pages), ErrorCount: int64(r.Errors), ChunkCount: int64(r.Chunks), Urls: urls}
}

// ListReplays implements GET /v1/replays.
func (h *Handlers) ListReplays(ctx context.Context, req oapi.ListReplaysRequestObject) (oapi.ListReplaysResponseObject, error) {
	params := req.Params
	p, err := apikit.Project(ctx, params.ProjectId)
	if err != nil {
		return nil, err
	}
	out := gen.ReplayList{Replays: []gen.ReplaySummary{}, RetentionDays: h.svc.RetentionDays()}
	if !h.svc.Enabled() {
		return oapi.ListReplays200JSONResponse(out), nil
	}
	from, to := h.clock.TimeRange(params.From, params.To)
	f := ch.ReplayFilter{Project: p, From: from, To: to, Limit: apikit.Limit(params.Limit, 50, 200)}
	if params.HasErrors != nil {
		f.HasErrors = *params.HasErrors
	}
	if params.User != nil {
		f.DistinctID = *params.User
	}
	if params.Url != nil {
		f.URL = *params.Url
	}
	if params.MinDurationMs != nil {
		f.MinDurationMs = *params.MinDurationMs
	}
	rows, err := h.svc.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out.Replays = make([]gen.ReplaySummary, 0, len(rows))
	for _, row := range rows {
		out.Replays = append(out.Replays, replaySummary(row))
	}
	return oapi.ListReplays200JSONResponse(out), nil
}

// find resolves the session within the user's project.
func (h *Handlers) find(ctx context.Context, session string, pid *gen.ProjectID) (uint64, ch.ReplaySummary, error) {
	p, err := apikit.Project(ctx, pid)
	if err != nil {
		return 0, ch.ReplaySummary{}, err
	}
	sum, err := h.svc.Find(ctx, p, session)
	return p, sum, mapErr(err)
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return apikit.Err(http.StatusNotFound, "replay not found")
	case errors.Is(err, domain.ErrStorageOff):
		return apikit.Err(http.StatusServiceUnavailable, "replay storage is not configured")
	}
	return err
}

// GetReplay implements GET /v1/replays/{session_id}.
func (h *Handlers) GetReplay(ctx context.Context, req oapi.GetReplayRequestObject) (oapi.GetReplayResponseObject, error) {
	p, sum, err := h.find(ctx, req.SessionId, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	d, err := h.svc.Detail(ctx, p, req.SessionId)
	if err != nil {
		return nil, err
	}
	out := gen.ReplayDetail{Summary: replaySummary(sum), Errors: make([]gen.ErrorOccurrence, 0, len(d.Errors)),
		Traces: make([]gen.SessionTrace, 0, len(d.Traces)), RetentionDays: h.svc.RetentionDays()}
	for _, e := range d.Errors {
		out.Errors = append(out.Errors, apikit.ErrorOccurrence(e))
	}
	for _, t := range d.Traces {
		out.Traces = append(out.Traces, gen.SessionTrace{TraceId: t.TraceID, Start: t.Start, Name: t.Name, ServiceName: t.Service,
			DurationNs: int64(t.DurationNs), SpanCount: int64(t.Spans), ErrorCount: int64(t.ErrorSpans)})
	}
	return oapi.GetReplay200JSONResponse(out), nil
}

// GetReplayEvents implements GET /v1/replays/{session_id}/events, streaming
// every chunk's events as one JSON array.
func (h *Handlers) GetReplayEvents(ctx context.Context, req oapi.GetReplayEventsRequestObject) (oapi.GetReplayEventsResponseObject, error) {
	p, _, err := h.find(ctx, req.SessionId, req.Params.ProjectId)
	if err != nil {
		return nil, err
	}
	keys, err := h.svc.Chunks(ctx, p, req.SessionId)
	if err != nil {
		return nil, mapErr(err)
	}
	return eventStream{ctx: ctx, svc: h.svc, keys: keys}, nil
}

// eventStream writes the events of every chunk as one JSON array.
type eventStream struct {
	ctx  context.Context
	svc  *service.Service
	keys []string
}

func (s eventStream) VisitGetReplayEventsResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write([]byte("["))
	first := true
	s.svc.EachEvent(s.ctx, s.keys, func(e json.RawMessage) {
		if !first {
			_, _ = w.Write([]byte(","))
		}
		first = false
		_, _ = w.Write(e)
	})
	_, _ = w.Write([]byte("]"))
	return nil
}
