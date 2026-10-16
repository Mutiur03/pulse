// Package audit records security-relevant actions (logins, key and token
// changes, invites, role changes) in Postgres for organization owners.
package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Actions recorded in the log.
//
//nolint:gosec // action names, not credentials
const (
	LoginSuccess       = "login.success"
	LoginFailure       = "login.failure"
	LoginBlocked       = "login.blocked"
	Signup             = "signup"
	LogoutAll          = "logout.all"
	PasswordReset      = "password.reset"
	KeyCreate          = "key.create"
	KeyRotate          = "key.rotate"
	KeyRevoke          = "key.revoke"
	UploadTokenCreate  = "upload_token.create"
	UploadTokenRevoke  = "upload_token.revoke"
	InviteCreate       = "invite.create"
	InviteRevoke       = "invite.revoke"
	InviteAccept       = "invite.accept"
	MemberRemove       = "member.remove"
	MemberRole         = "member.role"
	ProjectCreate      = "project.create"
	ProjectUpdate      = "project.update"
	AlertChannelCreate = "alert_channel.create"
	AlertChannelDelete = "alert_channel.delete"
)

// Entry is one audit record. OrgID 0 means a user-level event such as a
// login; those show in every organization the user belongs to.
type Entry struct {
	ID         int64
	CreatedAt  time.Time
	OrgID      int64
	ProjectID  int64
	ActorID    int64
	ActorEmail string
	Action     string
	Target     string
	IP         string
	Details    map[string]any
}

// Recorder is what handlers need; Store implements it.
type Recorder interface {
	Record(ctx context.Context, e Entry) error
}

// Store keeps the log in the audit_log table.
type Store struct{ DB *pgxpool.Pool }

func nullable(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// Record inserts an entry. Without an actor id the user is looked up by
// email, so failed logins against an existing account are attributed to it.
// Without an organization, a project's entry goes to the project's.
func (s *Store) Record(ctx context.Context, e Entry) error {
	details := e.Details
	if details == nil {
		details = map[string]any{}
	}
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO audit_log (org_id, project_id, actor_id, actor_email, action, target, ip, details)
		VALUES (COALESCE($1, (SELECT org_id FROM projects WHERE id = $2)), $2, COALESCE($3, (SELECT id FROM users WHERE lower(email) = lower($4::text) LIMIT 1)), $4, $5, $6, $7, $8)`,
		nullable(e.OrgID), nullable(e.ProjectID), nullable(e.ActorID), e.ActorEmail, e.Action, e.Target, e.IP, b)
	return err
}

// List returns the newest entries of an organization, including user-level
// events of its current members, older than before (0 for the newest).
func (s *Store) List(ctx context.Context, org int64, limit int, before int64) ([]Entry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.Query(ctx, `SELECT a.id, a.created_at, COALESCE(a.org_id, 0), COALESCE(a.project_id, 0), COALESCE(a.actor_id, 0),
			a.actor_email, a.action, a.target, a.ip, a.details
		FROM audit_log a
		WHERE (a.org_id = $1 OR (a.org_id IS NULL AND a.actor_id IN (SELECT user_id FROM org_members WHERE org_id = $1)))
		  AND ($2 = 0 OR a.id < $2)
		ORDER BY a.id DESC LIMIT $3`, org, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var raw []byte
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.OrgID, &e.ProjectID, &e.ActorID, &e.ActorEmail, &e.Action, &e.Target, &e.IP, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &e.Details)
		out = append(out, e)
	}
	return out, rows.Err()
}
