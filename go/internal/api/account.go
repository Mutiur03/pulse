package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/orgs"
)

func meOf(p *auth.Principal) gen.Me {
	out := gen.Me{
		User:     gen.User{Id: p.User.ID, Email: p.User.Email, Name: p.User.Name, EmailVerified: p.User.EmailVerified},
		Projects: []gen.Project{}, Orgs: []gen.Org{},
	}
	for _, pr := range p.Projects {
		out.Projects = append(out.Projects, project2gen(pr))
	}
	for _, o := range p.Orgs {
		out.Orgs = append(out.Orgs, gen.Org{Id: o.ID, Name: o.Name, Role: gen.OrgRole(o.Role)})
	}
	return out
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var body gen.LoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil || body.Email == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "email and password are required")
		return
	}
	ctx, ip, l := r.Context(), s.ip(r), s.Limits
	if ok, wait := l.LoginIP.Take(ctx, ip); !ok {
		tooMany(w, wait, "too many login attempts from this address, try again later")
		return
	}
	blocked, wait := l.LoginFail.Blocked(ctx, body.Email, ip)
	if !blocked {
		blocked, wait = l.LoginEmail.Blocked(ctx, body.Email)
	}
	if blocked {
		s.record(r, audit.Entry{ActorEmail: body.Email, Action: audit.LoginBlocked, IP: ip})
		tooMany(w, wait, "too many failed logins, try again later")
		return
	}
	token, u, err := s.Accounts.Login(ctx, body.Email, body.Password, r.UserAgent())
	if errors.Is(err, auth.ErrBadCredentials) {
		l.LoginFail.Fail(ctx, body.Email, ip)
		l.LoginEmail.Fail(ctx, body.Email)
		s.record(r, audit.Entry{ActorEmail: body.Email, Action: audit.LoginFailure, IP: ip})
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	l.LoginFail.Reset(ctx, body.Email, ip)
	l.LoginEmail.Reset(ctx, body.Email)
	s.record(r, audit.Entry{ActorID: u.ID, ActorEmail: u.Email, Action: audit.LoginSuccess, IP: ip,
		Details: map[string]any{"user_agent": truncate(r.UserAgent(), 200)}})
	s.endOldSession(r)
	s.startSession(w, r, token, http.StatusOK)
}

// endOldSession deletes the session the browser had before logging in, so
// a new login always gets a fresh token (session fixation).
func (s *Server) endOldSession(r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil && c.Value != "" {
		if err := s.Accounts.Logout(r.Context(), c.Value); err != nil {
			log.Printf("ending previous session: %v", err)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, token string, code int) {
	p, err := s.Accounts.Principal(r.Context(), token)
	if err != nil {
		internal(w, err)
		return
	}
	auth.SetCookie(w, token, s.SecureCookie)
	writeJSON(w, code, meOf(p))
}

func (s *Server) Signup(w http.ResponseWriter, r *http.Request) {
	var body gen.SignupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if ok, wait := s.Limits.SignupIP.Take(r.Context(), s.ip(r)); !ok {
		tooMany(w, wait, "too many signups from this address, try again later")
		return
	}
	invite := ""
	if body.InviteToken != nil {
		invite = *body.InviteToken
	}
	token, u, err := s.Accounts.Signup(r.Context(), body.Email, body.Password, body.Name, r.UserAgent(), invite)
	var verr *auth.ValidationError
	switch {
	case errors.As(err, &verr):
		writeErr(w, http.StatusBadRequest, verr.Msg)
		return
	case errors.Is(err, auth.ErrEmailTaken):
		writeErr(w, http.StatusConflict, "an account with this email already exists")
		return
	case errors.Is(err, orgs.ErrWrongUser):
		writeErr(w, http.StatusBadRequest, "this invite was sent to a different email address")
		return
	case errors.Is(err, orgs.ErrBadInvite):
		writeErr(w, http.StatusBadRequest, "the invite is invalid or has expired")
		return
	case err != nil:
		internal(w, err)
		return
	}
	if !u.EmailVerified {
		s.sendVerification(r.Context(), u)
	}
	s.record(r, audit.Entry{ActorID: u.ID, ActorEmail: u.Email, Action: audit.Signup, Details: map[string]any{"invited": invite != ""}})
	s.endOldSession(r)
	s.startSession(w, r, token, http.StatusCreated)
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		if err := s.Accounts.Logout(r.Context(), c.Value); err != nil {
			internal(w, err)
			return
		}
	}
	auth.ClearCookie(w, s.SecureCookie)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	writeJSON(w, http.StatusOK, meOf(p))
}

func apiKey(k keys.APIKey) gen.ApiKey {
	return gen.ApiKey{Id: k.ID, ProjectId: k.ProjectID, PublicKey: k.PublicKey, RateLimitPerSec: k.RateLimitPerSec,
		CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt}
}

func (s *Server) ListKeys(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	list, err := s.Keys.List(r.Context(), projectID)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.ApiKey, 0, len(list))
	for _, k := range list {
		out = append(out, apiKey(k))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) RotateKey(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !owns(w, r, projectID) {
		return
	}
	k, revoked, err := s.Keys.Rotate(r.Context(), projectID)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.KeyRotate, Target: keyLabel(k.PublicKey),
		Details: map[string]any{"revoked": len(revoked)}})
	for _, old := range revoked {
		if err := s.Keys.Purge(r.Context(), old); err != nil {
			// The cache entry still expires within keys.PositiveTTL.
			internal(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, apiKey(k))
}

// owns writes 404 if the user is not a member and 403 if not an owner.
func owns(w http.ResponseWriter, r *http.Request, projectID int64) bool {
	p := auth.FromContext(r.Context())
	if p == nil || !p.Can(projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return false
	}
	for _, pr := range p.Projects {
		if pr.ID == projectID && pr.Role != "owner" {
			writeErr(w, http.StatusForbidden, "only project owners can do this")
			return false
		}
	}
	return true
}

func (s *Server) CreateKey(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !owns(w, r, projectID) {
		return
	}
	k, err := s.Keys.Create(r.Context(), projectID)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.KeyCreate, Target: keyLabel(k.PublicKey)})
	writeJSON(w, http.StatusCreated, apiKey(k))
}

func (s *Server) RevokeKey(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, keyID int64) {
	if !owns(w, r, projectID) {
		return
	}
	k, err := s.Keys.Revoke(r.Context(), projectID, keyID)
	if errors.Is(err, keys.ErrKeyNotFound) {
		writeErr(w, http.StatusNotFound, "key not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.KeyRevoke, Target: keyLabel(k.PublicKey)})
	if err := s.Keys.Purge(r.Context(), k.PublicKey); err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiKey(k))
}

// keyLabel shortens a public key for the audit log.
func keyLabel(k string) string {
	if len(k) > 12 {
		return k[:12] + "..."
	}
	return k
}
