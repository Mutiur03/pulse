package api

import (
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/keys"
)

func meOf(p *auth.Principal) gen.Me {
	out := gen.Me{User: gen.User{Id: p.User.ID, Email: p.User.Email, Name: p.User.Name}, Projects: []gen.Project{}}
	for _, pr := range p.Projects {
		out.Projects = append(out.Projects, gen.Project{Id: pr.ID, Name: pr.Name, Role: gen.ProjectRole(pr.Role)})
	}
	return out
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var body gen.LoginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil || body.Email == "" || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "email and password are required")
		return
	}
	ip := clientIP(r)
	if blocked, wait := s.Throttle.Blocked(r.Context(), body.Email, ip); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		writeErr(w, http.StatusTooManyRequests, "too many failed logins, try again later")
		return
	}
	token, _, err := s.Accounts.Login(r.Context(), body.Email, body.Password, r.UserAgent())
	if errors.Is(err, auth.ErrBadCredentials) {
		s.Throttle.Fail(r.Context(), body.Email, ip)
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	s.Throttle.Reset(r.Context(), body.Email, ip)
	s.startSession(w, r, token, http.StatusOK)
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

// clientIP is the peer address. The web app proxies browser calls, so behind
// it this is the proxy's address and throttling is effectively per email.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) Signup(w http.ResponseWriter, r *http.Request) {
	var body gen.SignupRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	token, _, err := s.Accounts.Signup(r.Context(), body.Email, body.Password, body.Name, r.UserAgent())
	var verr *auth.ValidationError
	switch {
	case errors.As(err, &verr):
		writeErr(w, http.StatusBadRequest, verr.Msg)
		return
	case errors.Is(err, auth.ErrEmailTaken):
		writeErr(w, http.StatusConflict, "an account with this email already exists")
		return
	case err != nil:
		internal(w, err)
		return
	}
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
	if err := s.Keys.Purge(r.Context(), k.PublicKey); err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiKey(k))
}
