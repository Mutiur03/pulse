package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/modules/identity/domain"
	"github.com/Mutiur03/pulse/internal/orgs"
)

// ip is the client address, trusting X-Forwarded-For only from configured
// proxies (the web app).
func (s *Handlers) ip(r *http.Request) string {
	return s.ClientIP.IP(r)
}

func tooMany(w http.ResponseWriter, wait time.Duration, msg string) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Max(1, math.Ceil(wait.Seconds())))))
	writeErr(w, http.StatusTooManyRequests, msg)
}

// record writes an audit entry, filling in the actor and address. Failures
// are logged and do not fail the request.
func (s *Handlers) record(r *http.Request, e audit.Entry) {
	if s.Audit == nil {
		return
	}
	if p := auth.FromContext(r.Context()); p != nil && e.ActorID == 0 {
		e.ActorID, e.ActorEmail = p.User.ID, p.User.Email
	}
	if e.IP == "" {
		e.IP = s.ip(r)
	}
	if e.OrgID == 0 && e.ProjectID != 0 {
		if p := auth.FromContext(r.Context()); p != nil {
			for _, pr := range p.Projects {
				if pr.ID == e.ProjectID {
					e.OrgID = pr.OrgID
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if err := s.Audit.Record(ctx, e); err != nil {
		slog.Error("audit log write failed", "action", e.Action, "err", err)
	}
}

func (s *Handlers) LogoutAll(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	if s.Sessions == nil {
		writeErr(w, http.StatusServiceUnavailable, "sessions are not configured")
		return
	}
	n, err := s.Sessions.LogoutAll(r.Context(), p.User.ID)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{Action: audit.LogoutAll, Details: map[string]any{"sessions": n}})
	auth.ClearCookie(w, s.SecureCookie)
	writeJSON(w, http.StatusOK, gen.LogoutAllResult{SessionsEnded: n})
}

func (s *Handlers) SetMemberRole(w http.ResponseWriter, r *http.Request, org gen.OrgPath, user int64) {
	if _, ok := orgAccess(w, r, org, true); !ok {
		return
	}
	var body gen.RoleInput
	if !decode(w, r, &body) {
		return
	}
	if !domain.ValidRole(string(body.Role)) {
		writeErr(w, http.StatusBadRequest, "role must be owner or member")
		return
	}
	if s.Roles == nil {
		writeErr(w, http.StatusServiceUnavailable, "roles are not configured")
		return
	}
	err := s.Roles.SetRole(r.Context(), org, user, string(body.Role))
	switch {
	case errors.Is(err, orgs.ErrNotFound):
		writeErr(w, http.StatusNotFound, "member not found")
	case errors.Is(err, orgs.ErrLastOwner):
		writeErr(w, http.StatusConflict, "the organization needs at least one owner")
	case err != nil:
		internal(w, err)
	default:
		s.record(r, audit.Entry{OrgID: org, Action: audit.MemberRole, Target: "user " + strconv.FormatInt(user, 10),
			Details: map[string]any{"role": string(body.Role)}})
		w.WriteHeader(http.StatusNoContent)
	}
}
