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
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName = "pulse_session"
	// SessionTTL is the absolute lifetime of a session.
	SessionTTL = 7 * 24 * time.Hour
	// DefaultIdle ends sessions unused for this long.
	DefaultIdle = 24 * time.Hour
	// touchEvery limits last_seen_at writes to one per session per minute.
	touchEvery = time.Minute
	BcryptCost = 12
)

var (
	ErrBadCredentials = errors.New("invalid email or password")
	ErrNoSession      = errors.New("no valid session")
)

type User struct {
	ID            int64
	Email         string
	Name          string
	EmailVerified bool
}

type Project struct {
	ID    int64
	Name  string
	Role  string
	OrgID int64
}

// Org is an organization the user belongs to.
type Org struct {
	ID   int64
	Name string
	Role string
}

// Principal is the logged-in user, their organizations and the projects they
// can read.
type Principal struct {
	User     User
	Projects []Project
	Orgs     []Org
}

// OrgRole returns the user's role in the organization, or "" if not a member.
func (p *Principal) OrgRole(org int64) string {
	for _, o := range p.Orgs {
		if o.ID == org {
			return o.Role
		}
	}
	return ""
}

// ProjectsSQL selects (id, name, role, org_id) of every project a user ($1)
// can read: projects they are a direct member of and all projects of their
// organizations. Owner of either kind wins.
const ProjectsSQL = `SELECT p.id, p.name, CASE WHEN bool_or(x.role = 'owner') THEN 'owner' ELSE 'member' END, COALESCE(p.org_id, 0)
	FROM (SELECT project_id, role FROM project_members WHERE user_id = $1
	      UNION ALL
	      SELECT pr.id, om.role FROM org_members om JOIN projects pr ON pr.org_id = om.org_id WHERE om.user_id = $1) x
	JOIN projects p ON p.id = x.project_id
	GROUP BY p.id ORDER BY p.id`

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
type Store struct {
	DB *pgxpool.Pool
	// Idle ends sessions unused for longer; 0 means DefaultIdle.
	Idle time.Duration
}

func (s *Store) idle() time.Duration {
	if s.Idle > 0 {
		return s.Idle
	}
	return DefaultIdle
}

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
	token, err := newSession(ctx, s.DB, u.ID, userAgent)
	if err != nil {
		return "", User{}, err
	}
	return token, u, nil
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// newSession stores a session for the user and returns the raw token.
func newSession(ctx context.Context, db execer, userID int64, userAgent string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	_, err := db.Exec(ctx, `INSERT INTO user_sessions (token_hash, user_id, expires_at, user_agent)
		VALUES ($1, $2, $3::timestamptz, $4::text)`, tokenHash(token), userID, time.Now().Add(SessionTTL), userAgent)
	if err != nil {
		return "", err
	}
	return token, nil
}

// Principal resolves a session token to the user and their projects.
func (s *Store) Principal(ctx context.Context, token string) (*Principal, error) {
	if token == "" || len(token) > 100 {
		return nil, ErrNoSession
	}
	p := &Principal{}
	var lastSeen time.Time
	// A session ends at its absolute expiry or after Idle without use.
	err := s.DB.QueryRow(ctx, `SELECT u.id, u.email, u.name, u.email_verified_at IS NOT NULL, s.last_seen_at
		FROM user_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now() AND s.last_seen_at > now() - make_interval(secs => $2)`,
		tokenHash(token), s.idle().Seconds()).Scan(&p.User.ID, &p.User.Email, &p.User.Name, &p.User.EmailVerified, &lastSeen)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoSession
	}
	if err != nil {
		return nil, err
	}
	if time.Since(lastSeen) > touchEvery {
		if _, err := s.DB.Exec(ctx, `UPDATE user_sessions SET last_seen_at = now() WHERE token_hash = $1`, tokenHash(token)); err != nil {
			return nil, err
		}
	}
	if p.Projects, err = ListProjects(ctx, s.DB, p.User.ID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT o.id, o.name, m.role FROM org_members m JOIN organizations o ON o.id = m.org_id
		WHERE m.user_id = $1 ORDER BY o.id`, p.User.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o Org
		if err := rows.Scan(&o.ID, &o.Name, &o.Role); err != nil {
			return nil, err
		}
		p.Orgs = append(p.Orgs, o)
	}
	return p, rows.Err()
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ListProjects runs ProjectsSQL.
func ListProjects(ctx context.Context, db querier, userID int64) ([]Project, error) {
	rows, err := db.Query(ctx, ProjectsSQL, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var pr Project
		if err := rows.Scan(&pr.ID, &pr.Name, &pr.Role, &pr.OrgID); err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

// Logout deletes the session.
func (s *Store) Logout(ctx context.Context, token string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM user_sessions WHERE token_hash = $1`, tokenHash(token))
	return err
}

// LogoutAll deletes every session of the user, on every device.
func (s *Store) LogoutAll(ctx context.Context, userID int64) (int64, error) {
	tag, err := s.DB.Exec(ctx, `DELETE FROM user_sessions WHERE user_id = $1`, userID)
	return tag.RowsAffected(), err
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
var Public = map[string]bool{
	"/health": true, "/v1/auth/login": true, "/v1/auth/signup": true,
	"/v1/auth/password-reset/request": true, "/v1/auth/password-reset/confirm": true,
	"/v1/auth/verify-email": true, "/v1/invites/lookup": true,
}

// tokenUpload matches POST /v1/projects/{id}/releases/{release}/sourcemaps.
var tokenUpload = regexp.MustCompile(`^/v1/projects/[0-9]+/releases/[^/]+/sourcemaps$`)

// bearerUpload reports a source map upload carrying an upload token. Those
// skip the session (the handler checks the token), and since browsers never
// attach bearer tokens on their own they need no CSRF content type check.
func bearerUpload(r *http.Request) bool {
	return r.Method == http.MethodPost && tokenUpload.MatchString(r.URL.Path) &&
		strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// CSRF configures the cross-site request checks of Middleware.
type CSRF struct {
	// Origins are the web app origins (scheme://host[:port]) allowed to make
	// state-changing requests. Empty skips the Origin comparison; the
	// Sec-Fetch-Site and content type checks still apply.
	Origins []string
}

// NormalizeOrigin lowercases an origin and drops a trailing slash, or
// returns "" if s is not an http(s) origin.
func NormalizeOrigin(s string) string {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

// crossSite reports a state-changing request that a browser made from
// another site: a foreign Origin, or Sec-Fetch-Site cross-site when the
// Origin header is missing.
func (c CSRF) crossSite(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" || site == "same-site" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" || len(c.Origins) == 0 {
		return false
	}
	o := NormalizeOrigin(origin)
	for _, allowed := range c.Origins {
		if o != "" && o == allowed {
			return false
		}
	}
	return true
}

func mutating(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
}

// Middleware requires a valid session for everything but Public paths. For
// state-changing requests it also requires JSON (a cross-site form cannot
// send it without a CORS preflight, which the API never grants) and rejects
// requests whose Origin is not the web app or that a browser marks as
// cross-site. Together with SameSite=Lax cookies this is the CSRF defence.
func Middleware(store SessionStore, csrf CSRF) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if bearerUpload(r) {
				next.ServeHTTP(w, r)
				return
			}
			if mutating(r) && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				writeErr(w, http.StatusUnsupportedMediaType, "content type must be application/json")
				return
			}
			if mutating(r) && csrf.crossSite(r) {
				writeErr(w, http.StatusForbidden, "cross-site request refused")
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
	// Secure is off only for plain-HTTP local development (COOKIE_SECURE=false).
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is configurable, see above
		Name: CookieName, Value: token, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(SessionTTL.Seconds()),
	})
}

// ClearCookie expires the session cookie.
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: "", Path: "/", HttpOnly: true, Secure: secure, //nolint:gosec // as in SetCookie
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
}
