// Package postgres stores dashboards in Postgres.
package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Mutiur03/pulse/internal/modules/dashboards/domain"
)

// Store keeps dashboards in Postgres.
type Store struct{ DB *pgxpool.Pool }

const cols = `d.id, d.project_id, d.name, d.description, COALESCE(d.created_by, 0), COALESCE(NULLIF(u.name, ''), u.email, ''),
	d.shared, d.widgets, d.created_at, d.updated_at`

func scan(row pgx.Row) (domain.Dashboard, error) {
	var d domain.Dashboard
	var raw []byte
	err := row.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Description, &d.CreatedBy, &d.CreatorName, &d.Shared, &raw, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, domain.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(raw, &d.Widgets); err != nil {
		return d, err
	}
	return d, nil
}

// List returns the project's dashboards the user can see, newest first.
func (s *Store) List(ctx context.Context, project, user int64) ([]domain.Dashboard, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+cols+` FROM dashboards d LEFT JOIN users u ON u.id = d.created_by
		WHERE d.project_id = $1 AND (d.shared OR d.created_by = $2) ORDER BY d.updated_at DESC, d.id DESC LIMIT 200`, project, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Dashboard{}
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Get returns a dashboard the user can see.
func (s *Store) Get(ctx context.Context, project, id, user int64) (domain.Dashboard, error) {
	return scan(s.DB.QueryRow(ctx, `SELECT `+cols+` FROM dashboards d LEFT JOIN users u ON u.id = d.created_by
		WHERE d.id = $1 AND d.project_id = $2 AND (d.shared OR d.created_by = $3)`, id, project, user))
}

// Create saves a new dashboard owned by d.CreatedBy.
func (s *Store) Create(ctx context.Context, d domain.Dashboard) (domain.Dashboard, error) {
	raw, err := json.Marshal(d.Widgets)
	if err != nil {
		return d, err
	}
	var id int64
	if err := s.DB.QueryRow(ctx, `INSERT INTO dashboards (project_id, name, description, created_by, shared, widgets)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, d.ProjectID, d.Name, d.Description, d.CreatedBy, d.Shared, raw).Scan(&id); err != nil {
		return d, err
	}
	return s.Get(ctx, d.ProjectID, id, d.CreatedBy)
}

// Update replaces a dashboard's fields. The caller checks the user may edit.
func (s *Store) Update(ctx context.Context, d domain.Dashboard, user int64) (domain.Dashboard, error) {
	raw, err := json.Marshal(d.Widgets)
	if err != nil {
		return d, err
	}
	tag, err := s.DB.Exec(ctx, `UPDATE dashboards SET name = $3, description = $4, shared = $5, widgets = $6, updated_at = now()
		WHERE id = $1 AND project_id = $2`, d.ID, d.ProjectID, d.Name, d.Description, d.Shared, raw)
	if err != nil {
		return d, err
	}
	if tag.RowsAffected() == 0 {
		return d, domain.ErrNotFound
	}
	return s.Get(ctx, d.ProjectID, d.ID, user)
}

// Delete removes a dashboard. The caller checks the user may edit.
func (s *Store) Delete(ctx context.Context, project, id int64) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM dashboards WHERE id = $1 AND project_id = $2`, id, project)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
