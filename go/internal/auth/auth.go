// Package auth implements email/password login with server-side sessions
// kept in Postgres and an HTTP-only session cookie.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName = "pulse_session"
	SessionTTL = 7 * 24 * time.Hour
	BcryptCost = 12
)

var (
	ErrBadCredentials = errors.New("invalid email or password")
	ErrNoSession      = errors.New("no valid session")
)

type User struct {
	ID    int64
	Email string
	Name  string
}

type Project struct {
	ID   int64
	Name string
	Role string
}

// Principal is the logged-in user and the projects they can read.
type Principal struct {
	User     User
	Projects []Project
}

// Can reports whether the user is a member of the project.
func (p *Principal) Can(project int64) bool {
	for _, pr := range p.Projects {
		if pr.ID == project {
			return true
		}
	}
	return false
}

// HashPassword returns a bcrypt hash of the password.
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), BcryptCost)
	return string(b), err
}

// dummyHash is compared against when the email is unknown, so a login for a
// missing user costs the same as one with a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("pulse-timing-equaliser"), BcryptCost)

func tokenHash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Store is the Postgres-backed user and session store.
type Store struct{ DB *pgxpool.Pool }

// Login checks the password and creates a session, returning the raw token
// for the cookie. Only its SHA-256 is stored.
func (s *Store) Login(ctx context.Context, email, password, userAgent string) (string, User, error) {
	var u User
	var hash string
	err := s.DB.QueryRow(ctx, `SELECT id, email, name, password_hash FROM users WHERE lower(email) = lower($1::text)`,
		strings.TrimSpace(email)).Scan(&u.ID, &u.Email, &u.Name, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", User{}, ErrBadCredentials
	}
	if err != nil {
		return "", User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", User{}, ErrBadCredentials
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", User{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO user_sessions (token_hash, user_id, expires_at, user_agent)
		VALUES ($1, $2, $3::timestamptz, $4::text)`, tokenHash(token), u.ID, time.Now().Add(SessionTTL), userAgent)
	if err != nil {
		return "", User{}, err
	}
	return token, u, nil
}

// Principal resolves a session token to the user and their projects.
func (s *Store) Principal(ctx context.Context, token string) (*Principal, error) {
	if token == "" || len(token) > 100 {
		return nil, ErrNoSession
	}
	p := &Principal{}
	err := s.DB.QueryRow(ctx, `SELECT u.id, u.email, u.name FROM user_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, tokenHash(token)).Scan(&p.User.ID, &p.User.Email, &p.User.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT p.id, p.name, m.role FROM project_members m JOIN projects p ON p.id = m.project_id
		WHERE m.user_id = $1 ORDER BY p.id`, p.User.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pr Project
		if err := rows.Scan(&pr.ID, &pr.Name, &pr.Role); err != nil {
			return nil, err
		}
		p.Projects = append(p.Projects, pr)
	}
	return p, rows.Err()
}

// Logout deletes the session.
func (s *Store) Logout(ctx context.Context, token string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM user_sessions WHERE token_hash = $1`, tokenHash(token))
	return err
}

// SessionStore is what the middleware needs; an interface for tests.
type SessionStore interface {
	Principal(ctx context.Context, token string) (*Principal, error)
}

type ctxKey struct{}

// FromContext returns the principal set by Middleware.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// WithPrincipal stores a principal in the context (used by tests).
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// Public paths skip authentication.
var Public = map[string]bool{"/health": true, "/v1/auth/login": true}

// Middleware requires a valid session for everything but Public paths and
// rejects state-changing requests that are not JSON, which (with SameSite
// cookies) blocks cross-site form posts.
func Middleware(store SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
				!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeErr(w, http.StatusUnsupportedMediaType, "content type must be application/json")
				return
			}
			if Public[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			c, err := r.Cookie(CookieName)
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "not logged in")
				return
			}
			p, err := store.Principal(r.Context(), c.Value)
			if errors.Is(err, ErrNoSession) {
				writeErr(w, http.StatusUnauthorized, "session expired")
				return
			}
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "internal error")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}

// SetCookie writes the session cookie. secure should be true behind HTTPS.
func SetCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(SessionTTL.Seconds()),
	})
}

// ClearCookie expires the session cookie.
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
