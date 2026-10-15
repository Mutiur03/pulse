// Package orgs manages organization members and email invites.
package orgs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InviteTTL is how long an invite link works.
const InviteTTL = 7 * 24 * time.Hour

var (
	ErrNotFound      = errors.New("not found")
	ErrLastOwner     = errors.New("the organization needs at least one owner")
	ErrAlreadyMember = errors.New("already a member")
	ErrBadInvite     = errors.New("invite is invalid, used, revoked or expired")
	ErrWrongUser     = errors.New("invite was sent to a different email")
)

type Member struct {
	UserID   int64
	Email    string
	Name     string
	Role     string
	JoinedAt time.Time
}

type Invite struct {
	ID        int64
	OrgID     int64
	Email     string
	Role      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// InviteInfo is what the accept page shows.
type InviteInfo struct {
	OrgID   int64
	OrgName string
	Email   string
	Role    string
	Inviter string
}

// NewToken returns a random URL-safe token and its SHA-256.
func NewToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	t := base64.RawURLEncoding.EncodeToString(raw)
	return t, Hash(t), nil
}

// Hash is the stored form of a token.
func Hash(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

type Store struct{ DB *pgxpool.Pool }

func (s *Store) Members(ctx context.Context, org int64) ([]Member, error) {
	rows, err := s.DB.Query(ctx, `SELECT u.id, u.email, u.name, m.role, m.created_at FROM org_members m
		JOIN users u ON u.id = m.user_id WHERE m.org_id = $1 ORDER BY m.created_at, u.id`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// RemoveMember takes the user out of the organization and its projects. The
// member rows are locked so two owners cannot remove each other at once.
func (s *Store) RemoveMember(ctx context.Context, org, user int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT user_id, role FROM org_members WHERE org_id = $1 FOR UPDATE`, org)
	if err != nil {
		return err
	}
	owners, target := 0, ""
	for rows.Next() {
		var id int64
		var role string
		if err := rows.Scan(&id, &role); err != nil {
			rows.Close()
			return err
		}
		if role == "owner" {
			owners++
		}
		if id == user {
			target = role
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if target == "" {
		return ErrNotFound
	}
	if target == "owner" && owners <= 1 {
		return ErrLastOwner
	}
	if _, err := tx.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`, org, user); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_members WHERE user_id = $2
		AND project_id IN (SELECT id FROM projects WHERE org_id = $1)`, org, user); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateInvite stores an invite and returns it with the raw token for the
// email. Earlier pending invites for the same address are revoked.
func (s *Store) CreateInvite(ctx context.Context, org int64, email, role string, invitedBy int64) (Invite, string, error) {
	email = strings.TrimSpace(email)
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND lower(u.email) = lower($2))`, org, email).Scan(&exists); err != nil {
		return Invite{}, "", err
	}
	if exists {
		return Invite{}, "", ErrAlreadyMember
	}
	token, hash, err := NewToken()
	if err != nil {
		return Invite{}, "", err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Invite{}, "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE org_invites SET revoked_at = now() WHERE org_id = $1 AND lower(email) = lower($2)
		AND accepted_at IS NULL AND revoked_at IS NULL`, org, email); err != nil {
		return Invite{}, "", err
	}
	inv := Invite{OrgID: org, Email: email, Role: role}
	err = tx.QueryRow(ctx, `INSERT INTO org_invites (org_id, email, role, token_hash, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + make_interval(secs => $6)) RETURNING id, created_at, expires_at`,
		org, email, role, hash, invitedBy, InviteTTL.Seconds()).Scan(&inv.ID, &inv.CreatedAt, &inv.ExpiresAt)
	if err != nil {
		return Invite{}, "", err
	}
	return inv, token, tx.Commit(ctx)
}

func (s *Store) ListInvites(ctx context.Context, org int64) ([]Invite, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, org_id, email, role, created_at, expires_at FROM org_invites
		WHERE org_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC LIMIT 100`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var i Invite
		if err := rows.Scan(&i.ID, &i.OrgID, &i.Email, &i.Role, &i.CreatedAt, &i.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) RevokeInvite(ctx context.Context, org, id int64) error {
	tag, err := s.DB.Exec(ctx, `UPDATE org_invites SET revoked_at = now()
		WHERE id = $1 AND org_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL`, id, org)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Lookup describes a usable invite.
func (s *Store) Lookup(ctx context.Context, token string) (InviteInfo, error) {
	var i InviteInfo
	err := s.DB.QueryRow(ctx, `SELECT o.id, o.name, i.email, i.role, COALESCE(u.name, '')
		FROM org_invites i JOIN organizations o ON o.id = i.org_id LEFT JOIN users u ON u.id = i.invited_by
		WHERE i.token_hash = $1 AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now()`,
		Hash(token)).Scan(&i.OrgID, &i.OrgName, &i.Email, &i.Role, &i.Inviter)
	if errors.Is(err, pgx.ErrNoRows) {
		return InviteInfo{}, ErrBadInvite
	}
	return i, err
}

// AcceptTx uses up the invite inside tx and adds the user to the
// organization. The invite must be addressed to email. An existing member
// keeps their role unless the invite makes them an owner.
func AcceptTx(ctx context.Context, tx pgx.Tx, token string, userID int64, email string) (int64, error) {
	var id, org int64
	var to, role string
	err := tx.QueryRow(ctx, `SELECT id, org_id, email, role FROM org_invites WHERE token_hash = $1
		AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now() FOR UPDATE`, Hash(token)).Scan(&id, &org, &to, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrBadInvite
	}
	if err != nil {
		return 0, err
	}
	if !strings.EqualFold(strings.TrimSpace(to), strings.TrimSpace(email)) {
		return 0, ErrWrongUser
	}
	if _, err := tx.Exec(ctx, `UPDATE org_invites SET accepted_at = now() WHERE id = $1`, id); err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, user_id) DO UPDATE SET role = CASE WHEN EXCLUDED.role = 'owner' THEN 'owner' ELSE org_members.role END`,
		org, userID, role)
	if err != nil {
		return 0, err
	}
	// Following the emailed link proves the address.
	_, err = tx.Exec(ctx, `UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = $1`, userID)
	return org, err
}

// Accept is AcceptTx in its own transaction.
func (s *Store) Accept(ctx context.Context, token string, userID int64, email string) (int64, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	org, err := AcceptTx(ctx, tx, token, userID, email)
	if err != nil {
		return 0, err
	}
	return org, tx.Commit(ctx)
}
