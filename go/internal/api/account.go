package api

import (
	"encoding/json"
	"errors"
	"net/http"

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
	token, _, err := s.Accounts.Login(r.Context(), body.Email, body.Password, r.UserAgent())
	if errors.Is(err, auth.ErrBadCredentials) {
		writeErr(w, http.StatusUnauthorized, "invalid email or password")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	p, err := s.Accounts.Principal(r.Context(), token)
	if err != nil {
		internal(w, err)
		return
	}
	auth.SetCookie(w, token, s.SecureCookie)
	writeJSON(w, http.StatusOK, meOf(p))
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
	p := auth.FromContext(r.Context())
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	for _, pr := range p.Projects {
		if pr.ID == projectID && pr.Role != "owner" {
			writeErr(w, http.StatusForbidden, "only project owners can rotate keys")
			return
		}
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
