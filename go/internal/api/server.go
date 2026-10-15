// Package api implements the query API defined in schemas/openapi/api.yaml.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/alerts"
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/issues"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/orgs"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
)

// TraceReader and IssueStore are the storage dependencies, as interfaces so
// handlers can be tested without databases.
type TraceReader interface {
	SpansByTrace(ctx context.Context, project uint64, traceID string) ([]ch.SpanRecord, error)
	ErrorsByTrace(ctx context.Context, project uint64, traceID string) ([]ch.ErrorRecord, error)
	EventsByTrace(ctx context.Context, project uint64, traceID string) ([]ch.EventRecord, error)
	ErrorsByIssue(ctx context.Context, project, issueID uint64, limit int) ([]ch.ErrorRecord, error)
	ListTraces(ctx context.Context, f ch.TraceFilter) ([]ch.TraceSummaryRow, error)
	Services(ctx context.Context, project uint64, from, to time.Time) ([]string, error)
	ListEvents(ctx context.Context, f ch.EventFilter) ([]ch.ExploredEvent, error)
	EventNames(ctx context.Context, project uint64, from, to time.Time) ([]ch.EventNameCount, error)
	IssueTrends(ctx context.Context, project uint64, ids []uint64, now time.Time) (map[uint64][]int64, error)
	RunFunnel(ctx context.Context, f ch.FunnelQuery) (ch.FunnelData, error)
}

// Accounts logs users in and out.
type Accounts interface {
	Login(ctx context.Context, email, password, userAgent string) (string, auth.User, error)
	Signup(ctx context.Context, email, password, name, userAgent, invite string) (string, auth.User, error)
	RequestReset(ctx context.Context, email string) (string, auth.User, bool, error)
	ResetPassword(ctx context.Context, token, password string) error
	IssueVerify(ctx context.Context, userID int64) (string, error)
	VerifyEmail(ctx context.Context, token string) error
	Principal(ctx context.Context, token string) (*auth.Principal, error)
	Logout(ctx context.Context, token string) error
}

// KeyStore manages ingest keys; Purge drops revoked keys from the gateway cache.
type KeyStore interface {
	List(ctx context.Context, projectID int64) ([]keys.APIKey, error)
	Rotate(ctx context.Context, projectID int64) (keys.APIKey, []string, error)
	Create(ctx context.Context, projectID int64) (keys.APIKey, error)
	Revoke(ctx context.Context, projectID, keyID int64) (keys.APIKey, error)
	Purge(ctx context.Context, publicKey string) error
}

// ProjectStore creates, lists and renames projects.
type ProjectStore interface {
	List(ctx context.Context, userID int64) ([]auth.Project, error)
	Create(ctx context.Context, userID, orgID int64, name string) (auth.Project, keys.APIKey, error)
	Rename(ctx context.Context, projectID int64, name string) (string, error)
}

// OrgStore manages organization members and invites.
type OrgStore interface {
	Members(ctx context.Context, org int64) ([]orgs.Member, error)
	RemoveMember(ctx context.Context, org, user int64) error
	CreateInvite(ctx context.Context, org int64, email, role string, invitedBy int64) (orgs.Invite, string, error)
	ListInvites(ctx context.Context, org int64) ([]orgs.Invite, error)
	RevokeInvite(ctx context.Context, org, id int64) error
	Lookup(ctx context.Context, token string) (orgs.InviteInfo, error)
	Accept(ctx context.Context, token string, userID int64, email string) (int64, error)
}

type IssueStore interface {
	List(ctx context.Context, f issues.ListFilter) ([]issues.Issue, int64, error)
	Get(ctx context.Context, id int64) (issues.Issue, error)
	SetStatus(ctx context.Context, id int64, st issues.Status) (issues.Issue, error)
}

type Server struct {
	Traces   TraceReader
	Issues   IssueStore
	Accounts Accounts
	Keys     KeyStore
	Projects ProjectStore
	// Throttle limits failed logins; nil disables it.
	Throttle *auth.Throttle
	// ResetThrottle limits password reset requests; nil disables it.
	ResetThrottle *auth.Throttle
	Orgs          OrgStore
	// Mail sends reset, invite and verification emails; nil drops them.
	Mail mail.Sender
	// AppURL is the web app's public base URL used in email links.
	AppURL string
	// Replays and ReplayObjects serve session replay; nil disables it.
	Replays       ReplayReader
	ReplayObjects ObjectGetter
	// ReplayRetentionDays is shown to users; 0 means the default.
	ReplayRetentionDays int
	// Alerts manages alert rules and channels; nil disables alerting.
	Alerts       AlertStore
	AlertMetrics AlertMetrics
	// AlertSender delivers test notifications.
	AlertSender interface {
		Send(ctx context.Context, c alerts.Channel, p alerts.Payload) error
	}
	// Releases, ArtifactObjects and Symbolicator serve source maps; nil
	// Releases disables them.
	Releases        ReleaseStore
	ArtifactObjects ObjectPutter
	Symbolicator    *symbolicate.Symbolicator
	SecureCookie    bool
	Now             func() time.Time
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
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

// project resolves the requested project for the logged-in user: the given
// id if they are a member, else their first project when none was given.
// It writes a 404 and returns false when the user cannot read it, so other
// projects' existence is not revealed.
func project(w http.ResponseWriter, r *http.Request, p *gen.ProjectID) (uint64, bool) {
	pr := auth.FromContext(r.Context())
	if pr == nil {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return 0, false
	}
	if p == nil {
		if len(pr.Projects) == 0 {
			writeErr(w, http.StatusNotFound, "no projects")
			return 0, false
		}
		return uint64(pr.Projects[0].ID), true
	}
	if !pr.Can(*p) {
		writeErr(w, http.StatusNotFound, "project not found")
		return 0, false
	}
	return uint64(*p), true
}

// timeRange applies the default of the last 24 hours.
func (s *Server) timeRange(from, to *time.Time) (time.Time, time.Time) {
	end := s.now().UTC()
	if to != nil {
		end = *to
	}
	start := end.Add(-24 * time.Hour)
	if from != nil {
		start = *from
	}
	return start, end
}

func limit(l *int, def, max int) int {
	if l == nil || *l < 1 {
		return def
	}
	if *l > max {
		return max
	}
	return *l
}

func (s *Server) GetHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, gen.Health{Status: "healthy", Service: "query-api"})
}

func (s *Server) GetTrace(w http.ResponseWriter, r *http.Request, traceID string, params gen.GetTraceParams) {
	traceID = strings.ToLower(traceID)
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
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
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	f := issues.ListFilter{ProjectID: int64(p), Limit: 25}
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
	ids := make([]uint64, len(list))
	for i, iss := range list {
		ids[i] = uint64(iss.ID)
	}
	trends, err := s.Traces.IssueTrends(r.Context(), p, ids, s.now())
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.IssueList{Issues: make([]gen.Issue, 0, len(list)), Total: total, Limit: f.Limit, Offset: f.Offset}
	for _, i := range list {
		a := toAPI(i)
		if t, ok := trends[uint64(i.ID)]; ok {
			a.Trend = &t
		}
		out.Issues = append(out.Issues, a)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetIssue(w http.ResponseWriter, r *http.Request, id gen.IssueID, params gen.GetIssueParams) {
	iss, err := s.Issues.Get(r.Context(), id)
	if err == nil && !canRead(r, iss.ProjectID) {
		err = issues.ErrNotFound
	}
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
	again := params.Symbolicate != nil && *params.Symbolicate
	for _, e := range recs {
		o := errorOccurrence(e)
		frames, mapped := s.stackFrames(r.Context(), iss.ProjectID, e, again)
		o.Frames, o.MappedFrames = toFrames(frames), &mapped
		out.RecentEvents = append(out.RecentEvents, o)
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
	cur, err := s.Issues.Get(r.Context(), id)
	if errors.Is(err, issues.ErrNotFound) || (err == nil && !canRead(r, cur.ProjectID)) {
		writeErr(w, http.StatusNotFound, "issue not found")
		return
	} else if err != nil {
		internal(w, err)
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

func canRead(r *http.Request, project int64) bool {
	p := auth.FromContext(r.Context())
	return p != nil && p.Can(project)
}
