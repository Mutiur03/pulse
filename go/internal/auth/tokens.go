package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/orgs"
	"github.com/jackc/pgx/v5"
)

const (
	ResetTTL  = time.Hour
	VerifyTTL = 7 * 24 * time.Hour
)

// ErrBadToken means an email token is unknown, used or expired.
var ErrBadToken = errors.New("link is invalid or has expired")

func (s *Store) issue(ctx context.Context, userID int64, kind string, ttl time.Duration) (string, error) {
	token, hash, err := orgs.NewToken()
	if err != nil {
		return "", err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO email_tokens (token_hash, kind, user_id, expires_at) VALUES ($1, $2, $3, $4::timestamptz)`,
		hash, kind, userID, time.Now().Add(ttl))
	return token, err
}

// RequestReset issues a reset token for the email's account. ok is false when
// there is no such account; callers must not reveal that.
func (s *Store) RequestReset(ctx context.Context, email string) (token string, u User, ok bool, err error) {
	err = s.DB.QueryRow(ctx, `SELECT id, email, name FROM users WHERE lower(email) = lower($1::text)`,
		strings.TrimSpace(email)).Scan(&u.ID, &u.Email, &u.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", User{}, false, nil
	}
	if err != nil {
		return "", User{}, false, err
	}
	token, err = s.issue(ctx, u.ID, "reset", ResetTTL)
	return token, u, err == nil, err
}

// IssueVerify issues an email verification token.
func (s *Store) IssueVerify(ctx context.Context, userID int64) (string, error) {
	return s.issue(ctx, userID, "verify", VerifyTTL)
}

func consume(ctx context.Context, tx pgx.Tx, token, kind string) (int64, error) {
	var user int64
	err := tx.QueryRow(ctx, `UPDATE email_tokens SET used_at = now()
		WHERE token_hash = $1 AND kind = $2 AND used_at IS NULL AND expires_at > now() RETURNING user_id`,
		orgs.Hash(token), kind).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrBadToken
	}
	return user, err
}

// ResetPassword sets a new password with a reset token, ends every session
// of the user and invalidates their other outstanding reset links.
func (s *Store) ResetPassword(ctx context.Context, token, password string) error {
	if err := CheckPassword(password); err != nil {
		return err
	}
	if token == "" || len(token) > 100 {
		return ErrBadToken
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	user, err := consume(ctx, tx, token, "reset")
	if err != nil {
		return err
	}
	// The reset link was delivered to the address, so it is verified too.
	if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, email_verified_at = COALESCE(email_verified_at, now())
		WHERE id = $1`, user, hash); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_sessions WHERE user_id = $1`, user); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE email_tokens SET used_at = now() WHERE user_id = $1 AND kind = 'reset' AND used_at IS NULL`, user); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VerifyEmail marks the token's user as verified.
func (s *Store) VerifyEmail(ctx context.Context, token string) error {
	if token == "" || len(token) > 100 {
		return ErrBadToken
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	user, err := consume(ctx, tx, token, "verify")
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = $1`, user); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
