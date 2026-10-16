package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/orgs"
)

func meOf(p *auth.Principal) gen.Me {
	out := gen.Me{
		User:     gen.User{Id: p.User.ID, Email: p.User.Email, Name: p.User.Name, EmailVerified: p.User.EmailVerified},
		Projects: []gen.Project{}, Orgs: []gen.Org{},
	}
	for _, pr := range p.Projects {
		out.Projects = append(out.Projects, gen.Project{Id: pr.ID, Name: pr.Name, Role: gen.ProjectRole(pr.Role), OrgId: pr.OrgID})
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
			slog.Error("ending previous session", "err", err)
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
