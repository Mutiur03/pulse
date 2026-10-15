package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/mail"
)

func (s *Server) link(path, token string) string {
	base := strings.TrimRight(s.AppURL, "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	return base + path + "?token=" + url.QueryEscape(token)
}

func (s *Server) send(m mail.Message) {
	if s.Mail == nil {
		return
	}
	mail.Async(s.Mail, m)
}

func (s *Server) sendVerification(ctx context.Context, u auth.User) {
	token, err := s.Accounts.IssueVerify(ctx, u.ID)
	if err != nil {
		log.Printf("issue verification token: %v", err)
		return
	}
	s.send(mail.Message{To: u.Email, Subject: "Confirm your email for Pulse", Text: "Hi " + u.Name + ",\n\n" +
		"Confirm your email address by opening this link:\n\n" + s.link("/verify-email", token) + "\n\n" +
		"If you did not create a Pulse account, you can ignore this email.\n"})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (s *Server) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var body gen.PasswordResetRequest
	if !decode(w, r, &body) {
		return
	}
	ip := clientIP(r)
	if blocked, wait := s.ResetThrottle.Blocked(r.Context(), body.Email, ip); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		writeErr(w, http.StatusTooManyRequests, "too many reset requests, try again later")
		return
	}
	s.ResetThrottle.Fail(r.Context(), body.Email, ip)
	if auth.ValidEmail(strings.TrimSpace(body.Email)) {
		token, u, ok, err := s.Accounts.RequestReset(r.Context(), body.Email)
		if err != nil {
			internal(w, err)
			return
		}
		if ok {
			s.send(mail.Message{To: u.Email, Subject: "Reset your Pulse password", Text: "Hi " + u.Name + ",\n\n" +
				"Someone asked to reset the password for your Pulse account. Open this link within an hour to choose a new one:\n\n" +
				s.link("/reset-password", token) + "\n\n" +
				"If it was not you, ignore this email; your password stays the same.\n"})
		}
	}
	// Same answer whether or not the account exists.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) VerifyEmail(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) ResendVerification(w http.ResponseWriter, r *http.Request) {
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
