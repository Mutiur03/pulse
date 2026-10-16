package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

func (s *Server) ListEvents(w http.ResponseWriter, r *http.Request, params gen.ListEventsParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	f := ch.EventFilter{Project: p, Limit: limit(params.Limit, 50, 200)}
	f.From, f.To = s.timeRange(params.From, params.To)
	for dst, src := range map[*string]*string{&f.Event: params.Event, &f.DistinctID: params.DistinctId, &f.SessionID: params.SessionId} {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	rows, err := s.Traces.ListEvents(r.Context(), f)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.EventList{Events: make([]gen.ProductEvent, 0, len(rows))}
	for _, e := range rows {
		pe := productEvent(e.EventRecord)
		pe.TraceId = strPtr(e.TraceID)
		out.Events = append(out.Events, pe)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ListEventNames(w http.ResponseWriter, r *http.Request, params gen.ListEventNamesParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	from, to := s.timeRange(params.From, params.To)
	rows, err := s.Traces.EventNames(r.Context(), p, from, to)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.EventName, 0, len(rows))
	for _, n := range rows {
		out = append(out, gen.EventName{Name: n.Name, Count: int64(n.Count)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) QueryFunnel(w http.ResponseWriter, r *http.Request, params gen.QueryFunnelParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	var body gen.FunnelQuery
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	q := ch.FunnelQuery{Project: p, Steps: body.Steps, WindowSeconds: 3600}
	if body.WindowSeconds != nil {
		q.WindowSeconds = *body.WindowSeconds
	}
	// Default range: the last 7 days.
	end := s.now().UTC()
	if body.To != nil {
		end = *body.To
	}
	start := end.Add(-7 * 24 * time.Hour)
	if body.From != nil {
		start = *body.From
	}
	q.From, q.To = start, end
	if err := q.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	d, err := s.Traces.RunFunnel(r.Context(), q)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, BuildFunnel(q, d))
}
