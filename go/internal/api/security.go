package api

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
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/orgs"
)

// AuthLimits are the per-address and per-account limits on the public auth
// endpoints. Any of them may be nil.
type AuthLimits struct {
	// LoginFail locks an email from one address after repeated failures.
	LoginFail *auth.Throttle
	// LoginEmail locks an email after failures from any addresses, so
	// rotating addresses does not give unlimited guesses.
	LoginEmail *auth.Throttle
	// LoginIP, SignupIP and ResetIP count every attempt per address.
	LoginIP  *auth.Throttle
	SignupIP *auth.Throttle
	ResetIP  *auth.Throttle
	// ResetEmail limits reset emails per address and email.
	ResetEmail *auth.Throttle
}

// SessionRevoker ends all sessions of a user.
type SessionRevoker interface {
	LogoutAll(ctx context.Context, userID int64) (int64, error)
}

// RoleStore changes organization roles.
type RoleStore interface {
	SetRole(ctx context.Context, org, user int64, role string) error
}

// AuditReader lists an organization's audit log.
// OriginStore reads and replaces a project's allowed browser origins and
// drops its keys from the ingest cache so changes apply at once.
type OriginStore interface {
	AllowedOrigins(ctx context.Context, project int64) ([]string, error)
	SetAllowedOrigins(ctx context.Context, project int64, origins []string) ([]string, error)
}

// ip is the client address, trusting X-Forwarded-For only from configured
// proxies (the web app).
func (s *Server) ip(r *http.Request) string {
	return s.ClientIP.IP(r)
}

func tooMany(w http.ResponseWriter, wait time.Duration, msg string) {
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Max(1, math.Ceil(wait.Seconds())))))
	writeErr(w, http.StatusTooManyRequests, msg)
}

// record writes an audit entry, filling in the actor and address. Failures
// are logged and do not fail the request.
func (s *Server) record(r *http.Request, e audit.Entry) {
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

func (s *Server) LogoutAll(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) SetMemberRole(w http.ResponseWriter, r *http.Request, org gen.OrgPath, user int64) {
	if _, ok := orgAccess(w, r, org, true); !ok {
		return
	}
	var body gen.RoleInput
	if !decode(w, r, &body) {
		return
	}
	if body.Role != gen.RoleInputRoleOwner && body.Role != gen.RoleInputRoleMember {
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

func (s *Server) GetAllowedOrigins(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if s.Origins == nil {
		writeJSON(w, http.StatusOK, gen.AllowedOrigins{Origins: []string{}})
		return
	}
	list, err := s.Origins.AllowedOrigins(r.Context(), projectID)
	if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gen.AllowedOrigins{Origins: list})
}

func (s *Server) SetAllowedOrigins(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !owns(w, r, projectID) {
		return
	}
	var body gen.AllowedOrigins
	if !decode(w, r, &body) {
		return
	}
	clean, err := keys.CleanOrigins(body.Origins)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Origins == nil {
		writeErr(w, http.StatusServiceUnavailable, "origins are not configured")
		return
	}
	saved, err := s.Origins.SetAllowedOrigins(r.Context(), projectID, clean)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.ProjectUpdate, Target: "allowed origins",
		Details: map[string]any{"origins": saved}})
	writeJSON(w, http.StatusOK, gen.AllowedOrigins{Origins: saved})
}
