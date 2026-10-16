package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/governance"
)

// GovernanceStore keeps data settings and deletion requests.
type GovernanceStore interface {
	Settings(ctx context.Context, project int64) (governance.Settings, error)
	SaveSettings(ctx context.Context, s governance.Settings) (governance.Settings, error)
	CreateRequest(ctx context.Context, r governance.Request) (governance.Request, error)
	ListRequests(ctx context.Context, project int64, limit int) ([]governance.Request, error)
	GetRequest(ctx context.Context, project, id int64) (governance.Request, error)
}

// PersonLinker finds every id linked to a person.
type PersonLinker interface {
	ResolvePerson(ctx context.Context, project uint64, distinctID string) (string, error)
	LinkedIDs(ctx context.Context, project uint64, ids []string) ([]string, error)
}

func (s *Server) governanceReady(w http.ResponseWriter) bool {
	if s.Governance == nil {
		writeErr(w, http.StatusServiceUnavailable, "data governance is not configured")
		return false
	}
	return true
}

func rulesOut(r governance.Rules) gen.ScrubRules {
	keys, patterns := r.Keys, r.Patterns
	if keys == nil {
		keys = []string{}
	}
	if patterns == nil {
		patterns = []string{}
	}
	return gen.ScrubRules{Emails: r.Emails, Cards: r.Cards, Ips: r.IPs, Keys: keys, Patterns: patterns}
}

func rulesIn(r gen.ScrubRules) governance.Rules {
	return governance.Rules{Emails: r.Emails, Cards: r.Cards, IPs: r.Ips, Keys: r.Keys, Patterns: r.Patterns}
}

func retentionOut(r governance.Retention) gen.RetentionSettings {
	return gen.RetentionSettings{EventsDays: r.EventsDays, TracesDays: r.TracesDays, LogsDays: r.LogsDays,
		ErrorsDays: r.ErrorsDays, ReplaysDays: r.ReplaysDays, SourcemapsDays: r.SourceMapsDays}
}

func retentionIn(r gen.RetentionSettings) governance.Retention {
	return governance.Retention{EventsDays: r.EventsDays, TracesDays: r.TracesDays, LogsDays: r.LogsDays,
		ErrorsDays: r.ErrorsDays, ReplaysDays: r.ReplaysDays, SourceMapsDays: r.SourcemapsDays}
}

func settingsOut(st governance.Settings) gen.DataSettings {
	out := gen.DataSettings{ProjectId: st.ProjectID, Scrubbing: rulesOut(st.Rules), Retention: retentionOut(st.Retention),
		Limits: retentionOut(governance.Caps), Saved: st.Saved}
	if st.Saved {
		t := st.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

func requestOut(r governance.Request) gen.DeletionRequest {
	ids := r.DistinctIDs
	if ids == nil {
		ids = []string{}
	}
	result := r.Result
	if result == nil {
		result = map[string]int64{}
	}
	return gen.DeletionRequest{Id: r.ID, ProjectId: r.ProjectID, DistinctId: r.DistinctID, PersonKey: r.PersonKey,
		DistinctIds: ids, Reason: r.Reason, Status: gen.DeletionStatus(r.Status), Result: result, Error: r.Error,
		Attempts: r.Attempts, CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt}
}

func (s *Server) GetDataSettings(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.governanceReady(w) {
		return
	}
	st, err := s.Governance.Settings(r.Context(), projectID)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsOut(st))
}

func (s *Server) UpdateDataSettings(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !owns(w, r, projectID) || !s.governanceReady(w) {
		return
	}
	var in gen.DataSettingsInput
	if !decode(w, r, &in) {
		return
	}
	st := governance.Settings{ProjectID: projectID, Rules: rulesIn(in.Scrubbing), Retention: retentionIn(in.Retention)}
	if err := st.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.Governance.SaveSettings(r.Context(), st)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: governance.ActionSettingsUpdate, Target: "data settings",
		Details: map[string]any{"scrubbing": rulesOut(saved.Rules), "retention": retentionOut(saved.Retention)}})
	writeJSON(w, http.StatusOK, settingsOut(saved))
}

func (s *Server) PreviewScrubbing(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	var in gen.ScrubPreviewInput
	if !decode(w, r, &in) {
		return
	}
	if len(in.Sample) > 10000 {
		writeErr(w, http.StatusBadRequest, "sample is longer than 10000 characters")
		return
	}
	rules, err := rulesIn(in.Scrubbing).Clean()
	if err == nil {
		var sc *governance.Scrubber
		if sc, err = governance.Compile(rules); err == nil {
			out := sc.String(in.Sample)
			if trimmed := strings.TrimSpace(in.Sample); strings.HasPrefix(trimmed, "{") && json.Valid([]byte(trimmed)) {
				out = string(sc.JSON([]byte(trimmed)))
			}
			writeJSON(w, http.StatusOK, gen.ScrubPreview{Scrubbed: out})
			return
		}
	}
	writeErr(w, http.StatusBadRequest, err.Error())
}

func (s *Server) ListDeletionRequests(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.governanceReady(w) {
		return
	}
	list, err := s.Governance.ListRequests(r.Context(), projectID, 200)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.DeletionRequest, 0, len(list))
	for _, q := range list {
		out = append(out, requestOut(q))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetDeletionRequest(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, requestID int64) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.governanceReady(w) {
		return
	}
	q, err := s.Governance.GetRequest(r.Context(), projectID, requestID)
	if errors.Is(err, governance.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "deletion request not found")
		return
	}
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, requestOut(q))
}

func (s *Server) CreateDeletionRequest(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !owns(w, r, projectID) || !s.governanceReady(w) {
		return
	}
	var in gen.DeletionRequestInput
	if !decode(w, r, &in) {
		return
	}
	id := strings.TrimSpace(in.DistinctId)
	reason := ""
	if in.Reason != nil {
		reason = strings.TrimSpace(*in.Reason)
	}
	if id == "" || len(id) > 200 {
		writeErr(w, http.StatusBadRequest, "distinct_id must be 1 to 200 characters")
		return
	}
	if len(reason) > 500 {
		writeErr(w, http.StatusBadRequest, "reason is longer than 500 characters")
		return
	}
	req := governance.Request{ProjectID: projectID, DistinctID: id, PersonKey: id, DistinctIDs: []string{id}, Reason: reason}
	if s.Persons != nil {
		key, err := s.Persons.ResolvePerson(r.Context(), uint64(projectID), id)
		if err != nil {
			internal(w, err)
			return
		}
		ids, err := s.Persons.LinkedIDs(r.Context(), uint64(projectID), []string{id, key})
		if err != nil {
			internal(w, err)
			return
		}
		req.PersonKey, req.DistinctIDs = key, ids
	}
	if p := auth.FromContext(r.Context()); p != nil {
		uid := p.User.ID
		req.RequestedBy = &uid
	}
	created, err := s.Governance.CreateRequest(r.Context(), req)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: governance.ActionDeletionRequest, Target: id,
		Details: map[string]any{"request_id": created.ID, "distinct_ids": created.DistinctIDs, "reason": reason}})
	writeJSON(w, http.StatusAccepted, requestOut(created))
}
