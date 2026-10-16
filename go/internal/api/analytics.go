package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/insights"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// AnalyticsReader serves persons, retention and paths.
type AnalyticsReader interface {
	ListPersons(ctx context.Context, f ch.PersonFilter) ([]ch.PersonRow, error)
	ResolvePerson(ctx context.Context, project uint64, distinctID string) (string, error)
	GetPerson(ctx context.Context, project uint64, person string, from, to time.Time) (ch.PersonRow, bool, error)
	PersonSessions(ctx context.Context, project uint64, person string, from, to time.Time, limit int) ([]ch.PersonSession, error)
	PersonEvents(ctx context.Context, project uint64, person string, from, to time.Time, limit int) ([]ch.EventRecord, error)
	RunRetention(ctx context.Context, q ch.RetentionQuery) (ch.RetentionData, error)
	RunPaths(ctx context.Context, q ch.PathsQuery) ([]ch.PathRow, error)
}

func (s *Server) analyticsReady(w http.ResponseWriter) bool {
	if s.Analytics == nil {
		writeErr(w, http.StatusServiceUnavailable, "analytics are not configured")
		return false
	}
	return true
}

// personRange defaults to the last 30 days, the event retention.
func (s *Server) personRange(from, to *time.Time) (time.Time, time.Time) {
	end := s.now().UTC()
	if to != nil {
		end = *to
	}
	start := end.Add(-30 * 24 * time.Hour)
	if from != nil {
		start = *from
	}
	return start, end
}

func personOut(p ch.PersonRow) gen.Person {
	traits := map[string]interface{}{}
	if p.Traits != "" {
		_ = json.Unmarshal([]byte(p.Traits), &traits)
	}
	// $-prefixed keys are SDK context, not traits the app set.
	for k := range traits {
		if strings.HasPrefix(k, "$") {
			delete(traits, k)
		}
	}
	ids := p.DistinctIDs
	if ids == nil {
		ids = []string{}
	}
	return gen.Person{
		Id: p.Person, DistinctIds: ids, FirstSeen: p.FirstSeen, LastSeen: p.LastSeen, Events: int64(p.Events),
		Sessions: int64(p.Sessions), Traits: traits, Browser: p.Browser, Os: p.OS, Country: p.Country, LastUrl: p.LastURL,
	}
}

func (s *Server) ListPersons(w http.ResponseWriter, r *http.Request, params gen.ListPersonsParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok || !s.analyticsReady(w) {
		return
	}
	f := ch.PersonFilter{Project: p, Limit: limit(params.Limit, 50, 200)}
	f.From, f.To = s.personRange(params.From, params.To)
	if params.Search != nil {
		f.Search = strings.TrimSpace(*params.Search)
		if len(f.Search) > 200 {
			writeErr(w, http.StatusBadRequest, "search is too long")
			return
		}
	}
	rows, err := s.Analytics.ListPersons(r.Context(), f)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.PersonList{Persons: make([]gen.Person, 0, len(rows))}
	for _, row := range rows {
		out.Persons = append(out.Persons, personOut(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetPerson(w http.ResponseWriter, r *http.Request, params gen.GetPersonParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok || !s.analyticsReady(w) {
		return
	}
	id := strings.TrimSpace(params.Id)
	if id == "" || len(id) > 200 {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	ctx := r.Context()
	from, to := s.personRange(params.From, params.To)
	key, err := s.Analytics.ResolvePerson(ctx, p, id)
	if err != nil {
		internal(w, err)
		return
	}
	row, found, err := s.Analytics.GetPerson(ctx, p, key, from, to)
	if err != nil {
		internal(w, err)
		return
	}
	if !found {
		writeErr(w, http.StatusNotFound, "person not found")
		return
	}
	sessions, err := s.Analytics.PersonSessions(ctx, p, key, from, to, 50)
	if err != nil {
		internal(w, err)
		return
	}
	events, err := s.Analytics.PersonEvents(ctx, p, key, from, to, 100)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.PersonDetail{Person: personOut(row), Sessions: make([]gen.PersonSession, 0, len(sessions)), Events: make([]gen.ProductEvent, 0, len(events))}
	for _, ps := range sessions {
		out.Sessions = append(out.Sessions, gen.PersonSession{
			SessionId: ps.SessionID, Start: ps.Start, End: ps.End, Events: int64(ps.Events), Pageviews: int64(ps.Pageviews),
			EntryPath: ps.EntryPath, ExitPath: ps.ExitPath, HasReplay: ps.Replay == 1, Errors: int64(ps.Errors),
		})
	}
	for _, e := range events {
		out.Events = append(out.Events, productEvent(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) QueryRetention(w http.ResponseWriter, r *http.Request, params gen.QueryRetentionParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	var body gen.RetentionQuery
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	q := ch.RetentionQuery{Project: p}
	if body.StartEvent != nil {
		q.StartEvent = strings.TrimSpace(*body.StartEvent)
	}
	if body.ReturnEvent != nil {
		q.ReturnEvent = strings.TrimSpace(*body.ReturnEvent)
	}
	if body.Period != nil {
		q.Period = string(*body.Period)
	}
	if body.Periods != nil {
		q.Periods = *body.Periods
	}
	now := s.now().UTC()
	q.To = now
	if body.To != nil {
		q.To = body.To.UTC()
	}
	// Default: as many periods back as the matrix is wide, plus one.
	back := 14 * 24 * time.Hour
	if q.Period == "week" {
		back = 8 * 7 * 24 * time.Hour
	}
	q.From = q.To.Add(-back)
	if body.From != nil {
		q.From = body.From.UTC()
	}
	if err := q.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.analyticsReady(w) {
		return
	}
	d, err := s.Analytics.RunRetention(r.Context(), q)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.RetentionResult{Period: gen.RetentionResultPeriod(q.Period), Periods: q.Periods, Cohorts: []gen.RetentionCohort{}}
	for _, c := range insights.BuildRetention(q, d, now) {
		out.Cohorts = append(out.Cohorts, gen.RetentionCohort{Start: c.Start, Size: c.Size, Values: c.Values})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) QueryPaths(w http.ResponseWriter, r *http.Request, params gen.QueryPathsParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	var body gen.PathsQuery
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	q := ch.PathsQuery{Project: p}
	if body.Mode != nil {
		q.Mode = string(*body.Mode)
	}
	if body.Start != nil {
		q.Start = strings.TrimSpace(*body.Start)
	}
	if body.Steps != nil {
		q.Steps = *body.Steps
	}
	q.To = s.now().UTC()
	if body.To != nil {
		q.To = body.To.UTC()
	}
	q.From = q.To.Add(-7 * 24 * time.Hour)
	if body.From != nil {
		q.From = body.From.UTC()
	}
	if err := q.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.analyticsReady(w) {
		return
	}
	rows, err := s.Analytics.RunPaths(r.Context(), q)
	if err != nil {
		internal(w, err)
		return
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
	writeJSON(w, http.StatusOK, out)
}
