// Package projects creates and renames projects inside organizations.
package projects

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the project does not exist or the user is not a member.
var ErrNotFound = errors.New("project not found")

// ErrBadName is returned for empty or overlong names.
var ErrBadName = errors.New("project name must be 1 to 80 characters")

// CleanName trims and validates a project name.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return "", ErrBadName
	}
	return name, nil
}

type Store struct{ DB *pgxpool.Pool }

// List returns the projects the user is a member of.
func (s *Store) List(ctx context.Context, userID int64) ([]auth.Project, error) {
	rows, err := s.DB.Query(ctx, `SELECT p.id, p.name, m.role FROM project_members m JOIN projects p ON p.id = m.project_id
		WHERE m.user_id = $1 ORDER BY p.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []auth.Project{}
	for rows.Next() {
		var p auth.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Role); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Create makes a project in the user's first organization (creating one for
// users that predate organizations), adds the user as owner and issues the
// first ingest key, in one transaction.
func (s *Store) Create(ctx context.Context, userID int64, name string) (auth.Project, keys.APIKey, error) {
	name, err := CleanName(name)
	if err != nil {
		return auth.Project{}, keys.APIKey{}, err
	}
	pk, err := keys.NewPublicKey()
	if err != nil {
		return auth.Project{}, keys.APIKey{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return auth.Project{}, keys.APIKey{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var org int64
	err = tx.QueryRow(ctx, `SELECT org_id FROM org_members WHERE user_id = $1
		ORDER BY role = 'owner' DESC, org_id LIMIT 1`, userID).Scan(&org)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.QueryRow(ctx, `INSERT INTO organizations (name)
			SELECT name || '''s organization' FROM users WHERE id = $1 RETURNING id`, userID).Scan(&org); err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, org, userID)
		}
	}
	if err != nil {
		return auth.Project{}, keys.APIKey{}, err
	}
	p := auth.Project{Name: name, Role: "owner"}
	if err := tx.QueryRow(ctx, `INSERT INTO projects (name, org_id) VALUES ($1, $2) RETURNING id`, name, org).Scan(&p.ID); err != nil {
		return auth.Project{}, keys.APIKey{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`, p.ID, userID); err != nil {
		return auth.Project{}, keys.APIKey{}, err
	}
	var k keys.APIKey
	err = tx.QueryRow(ctx, `INSERT INTO api_keys (project_id, public_key) VALUES ($1, $2)
		RETURNING id, project_id, public_key, rate_limit_per_sec, created_at, revoked_at`, p.ID, pk).
		Scan(&k.ID, &k.ProjectID, &k.PublicKey, &k.RateLimitPerSec, &k.CreatedAt, &k.RevokedAt)
	if err != nil {
		return auth.Project{}, keys.APIKey{}, err
	}
	return p, k, tx.Commit(ctx)
}

// Rename changes a project's name. The caller checks ownership.
func (s *Store) Rename(ctx context.Context, projectID int64, name string) (string, error) {
	name, err := CleanName(name)
	if err != nil {
		return "", err
	}
	tag, err := s.DB.Exec(ctx, `UPDATE projects SET name = $2, updated_at = now() WHERE id = $1`, projectID, name)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	return name, nil
}
