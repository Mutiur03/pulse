package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/mail"
	"github.com/Mutiur03/pulse/internal/modules/identity/domain"
)

func (s *Handlers) link(path, token string) string {
	base := strings.TrimRight(s.AppURL, "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	return base + path + "?token=" + url.QueryEscape(token)
}

func (s *Handlers) send(m mail.Message) {
	if s.Mail == nil {
		return
	}
	mail.Async(s.Mail, m)
}

func (s *Handlers) sendVerification(ctx context.Context, u auth.User) {
	token, err := s.Accounts.IssueVerify(ctx, u.ID)
	if err != nil {
		slog.Error("issue verification token", "err", err)
		return
	}
	s.send(domain.VerificationMail(u, s.link("/verify-email", token)))
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (s *Handlers) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var body gen.PasswordResetRequest
	if !decode(w, r, &body) {
		return
	}
	ip := s.ip(r)
	if ok, wait := s.Limits.ResetIP.Take(r.Context(), ip); !ok {
		tooMany(w, wait, "too many reset requests from this address, try again later")
		return
	}
	if blocked, wait := s.Limits.ResetEmail.Blocked(r.Context(), body.Email, ip); blocked {
		tooMany(w, wait, "too many reset requests, try again later")
		return
	}
	s.Limits.ResetEmail.Fail(r.Context(), body.Email, ip)
	if auth.ValidEmail(strings.TrimSpace(body.Email)) {
		token, u, ok, err := s.Accounts.RequestReset(r.Context(), body.Email)
		if err != nil {
			internal(w, err)
			return
		}
		if ok {
			s.send(domain.ResetMail(u, s.link("/reset-password", token)))
		}
	}
	// Same answer whether or not the account exists.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Handlers) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var body gen.PasswordResetConfirm
	if !decode(w, r, &body) {
		return
	}
	err := s.Accounts.ResetPassword(r.Context(), body.Token, body.Password)
	var verr *auth.ValidationError
	switch {
	case errors.As(err, &verr):
		writeErr(w, http.StatusBadRequest, verr.Msg)
	case errors.Is(err, auth.ErrBadToken):
		writeErr(w, http.StatusBadRequest, "the reset link is invalid or has expired")
	case err != nil:
		internal(w, err)
	default:
		// Any session this browser had is gone too.
		auth.ClearCookie(w, s.SecureCookie)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Handlers) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var body gen.TokenRequest
	if !decode(w, r, &body) {
		return
	}
	err := s.Accounts.VerifyEmail(r.Context(), body.Token)
	switch {
	case errors.Is(err, auth.ErrBadToken):
		writeErr(w, http.StatusBadRequest, "the verification link is invalid or has expired")
	case err != nil:
		internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Handlers) ResendVerification(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	if !p.User.EmailVerified {
		s.sendVerification(r.Context(), p.User)
	}
	w.WriteHeader(http.StatusNoContent)
}
