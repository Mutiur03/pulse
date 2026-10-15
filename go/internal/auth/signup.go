package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/Mutiur03/pulse/internal/orgs"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	MinPasswordLen = 10
	// bcrypt ignores everything after 72 bytes.
	MaxPasswordBytes = 72
)

var ErrEmailTaken = errors.New("email already registered")

// ValidationError is a user-facing problem with signup input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// NormalizeSignup trims the input and checks it, returning the cleaned email
// and name.
func NormalizeSignup(email, password, name string) (string, string, error) {
	if err := CheckPassword(password); err != nil {
		return "", "", err
	}
	email = strings.TrimSpace(email)
	name = strings.TrimSpace(name)
	if !ValidEmail(email) {
		return "", "", &ValidationError{"enter a valid email address"}
	}
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", "", &ValidationError{"name must be 1 to 100 characters"}
	}
	return email, name, nil
}

// CheckPassword enforces the password length rules.
func CheckPassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLen {
		return &ValidationError{"password must be at least 10 characters"}
	}
	if len(password) > MaxPasswordBytes {
		return &ValidationError{"password must be at most 72 bytes"}
	}
	return nil
}

// ValidEmail reports whether s is a bare address like a@example.com.
func ValidEmail(email string) bool {
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email && len(email) <= 254 &&
		strings.Contains(email[strings.LastIndex(email, "@")+1:], ".")
}

// Signup creates the user, an organization they own (or, with an invite
// token, membership of the inviting organization) and a session, all in one
// transaction. It returns the raw session token.
func (s *Store) Signup(ctx context.Context, email, password, name, userAgent, invite string) (string, User, error) {
	email, name, err := NormalizeSignup(email, password, name)
	if err != nil {
		return "", User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return "", User{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	u := User{Email: email, Name: name}
	err = tx.QueryRow(ctx, `INSERT INTO users (email, password_hash, name) VALUES ($1, $2, $3) RETURNING id`,
		email, hash, name).Scan(&u.ID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return "", User{}, ErrEmailTaken
	}
	if err != nil {
		return "", User{}, err
	}
	if invite != "" {
		if _, err := orgs.AcceptTx(ctx, tx, invite, u.ID, email); err != nil {
			return "", User{}, err
		}
		u.EmailVerified = true
	} else {
		var org int64
		if err := tx.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name+"'s organization").Scan(&org); err != nil {
			return "", User{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, org, u.ID); err != nil {
			return "", User{}, err
		}
	}
	token, err := newSession(ctx, tx, u.ID, userAgent)
	if err != nil {
		return "", User{}, err
	}
	return token, u, tx.Commit(ctx)
}
