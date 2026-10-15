// Package api implements the query API defined in schemas/openapi/api.yaml.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// TraceReader and IssueStore are the storage dependencies, as interfaces so
// handlers can be tested without databases.
type TraceReader interface {
	SpansByTrace(ctx context.Context, project uint64, traceID string) ([]ch.SpanRecord, error)
	ErrorsByTrace(ctx context.Context, project uint64, traceID string) ([]ch.ErrorRecord, error)
	EventsByTrace(ctx context.Context, project uint64, traceID string) ([]ch.EventRecord, error)
	ErrorsByIssue(ctx context.Context, project, issueID uint64, limit int) ([]ch.ErrorRecord, error)
}

type IssueStore interface {
	List(ctx context.Context, f issues.ListFilter) ([]issues.Issue, int64, error)
	Get(ctx context.Context, id int64) (issues.Issue, error)
	SetStatus(ctx context.Context, id int64, st issues.Status) (issues.Issue, error)
}

type Server struct {
	Traces TraceReader
	Issues IssueStore
}

var _ gen.ServerInterface = (*Server)(nil)

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, gen.Error{Error: msg})
}

func internal(w http.ResponseWriter, err error) {
	log.Printf("query api error: %v", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func project(p *gen.ProjectID) uint64 {
	if p == nil || *p < 1 {
		return 1
	}
	return uint64(*p)
}

func (s *Server) GetHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, gen.Health{Status: "healthy", Service: "query-api"})
}

func (s *Server) GetTrace(w http.ResponseWriter, r *http.Request, traceID string, params gen.GetTraceParams) {
	traceID = strings.ToLower(traceID)
	p := project(params.ProjectId)
	ctx := r.Context()
	spans, err := s.Traces.SpansByTrace(ctx, p, traceID)
	if err != nil {
		internal(w, err)
		return
	}
	errs, err := s.Traces.ErrorsByTrace(ctx, p, traceID)
	if err != nil {
		internal(w, err)
		return
	}
	events, err := s.Traces.EventsByTrace(ctx, p, traceID)
	if err != nil {
		internal(w, err)
		return
	}
	if len(spans) == 0 && len(errs) == 0 && len(events) == 0 {
		writeErr(w, http.StatusNotFound, "trace not found")
		return
	}
	writeJSON(w, http.StatusOK, BuildTrace(traceID, spans, errs, events))
}

func toAPI(i issues.Issue) gen.Issue {
	out := gen.Issue{
		Id: i.ID, ProjectId: i.ProjectID, Fingerprint: i.Fingerprint, Title: i.Title, ExceptionType: i.ExceptionType,
		Culprit: i.Culprit, Platform: i.Platform, ServiceName: i.ServiceName, Status: gen.IssueStatus(i.Status),
		FirstSeen: i.FirstSeen, LastSeen: i.LastSeen, Count: i.Count, SampleTraceId: i.SampleTraceID,
		SampleSpanId: strPtr(i.SampleSpanID), ResolvedAt: i.ResolvedAt, TimesRegressed: i.TimesRegressed,
	}
	return out
}

func (s *Server) ListIssues(w http.ResponseWriter, r *http.Request, params gen.ListIssuesParams) {
	f := issues.ListFilter{ProjectID: int64(project(params.ProjectId)), Limit: 25}
	if params.Status != nil {
		if !issues.Status(*params.Status).Valid() {
			writeErr(w, http.StatusBadRequest, "invalid status")
			return
		}
		f.Status = issues.Status(*params.Status)
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
			writeErr(w, http.StatusBadRequest, "limit must be 1-100")
			return
		}
		f.Limit = *params.Limit
	}
	if params.Offset != nil {
		if *params.Offset < 0 {
			writeErr(w, http.StatusBadRequest, "offset must be >= 0")
			return
		}
		f.Offset = *params.Offset
	}
	list, total, err := s.Issues.List(r.Context(), f)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.IssueList{Issues: make([]gen.Issue, 0, len(list)), Total: total, Limit: f.Limit, Offset: f.Offset}
	for _, i := range list {
		out.Issues = append(out.Issues, toAPI(i))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetIssue(w http.ResponseWriter, r *http.Request, id gen.IssueID, params gen.GetIssueParams) {
	iss, err := s.Issues.Get(r.Context(), id)
	if errors.Is(err, issues.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "issue not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	limit := 20
	if params.EventsLimit != nil && *params.EventsLimit >= 1 && *params.EventsLimit <= 100 {
		limit = *params.EventsLimit
	}
	recs, err := s.Traces.ErrorsByIssue(r.Context(), uint64(iss.ProjectID), uint64(iss.ID), limit)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.IssueDetail{Issue: toAPI(iss), RecentEvents: make([]gen.ErrorOccurrence, 0, len(recs))}
	for _, e := range recs {
		out.RecentEvents = append(out.RecentEvents, errorOccurrence(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) UpdateIssue(w http.ResponseWriter, r *http.Request, id gen.IssueID) {
	var body gen.IssueUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	st := issues.Status(body.Status)
	if !st.Valid() {
		writeErr(w, http.StatusBadRequest, "status must be unresolved, resolved or ignored")
		return
	}
	iss, err := s.Issues.SetStatus(r.Context(), id, st)
	if errors.Is(err, issues.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "issue not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPI(iss))
}
