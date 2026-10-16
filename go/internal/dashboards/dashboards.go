// Package dashboards stores custom dashboards: named grids of saved
// queries (widgets) that are private to their creator or shared with
// everyone who can read the project.
package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means the dashboard does not exist or the user cannot see it.
var ErrNotFound = errors.New("dashboard not found")

// Widget kinds.
const (
	KindTrend  = "trend"
	KindFunnel = "funnel"
	KindMetric = "metric"
	KindTraces = "traces"
)

// Limits on a dashboard.
const (
	MaxWidgets     = 30
	MaxWidgetBytes = 8 << 10
)

// Widget is a saved query shown on a dashboard. Query holds the request of
// its kind: a trend query, funnel steps, a metric query or trace filters.
type Widget struct {
	ID    string          `json:"id"`
	Title string          `json:"title"`
	Kind  string          `json:"kind"`
	Width int             `json:"width"`
	Query json.RawMessage `json:"query"`
}

// Dashboard is a dashboard with its widgets.
type Dashboard struct {
	ID          int64
	ProjectID   int64
	Name        string
	Description string
	CreatedBy   int64
	CreatorName string
	Shared      bool
	Widgets     []Widget
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// QueryCheck validates a widget's query for its kind.
type QueryCheck func(kind string, query json.RawMessage) error

// Clean validates and normalizes a dashboard's fields and widgets.
func Clean(d *Dashboard, check QueryCheck) error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || utf8.RuneCountInString(d.Name) > 120 {
		return errors.New("dashboard name must be 1 to 120 characters")
	}
	d.Description = strings.TrimSpace(d.Description)
	if utf8.RuneCountInString(d.Description) > 500 {
		return errors.New("description must be at most 500 characters")
	}
	if len(d.Widgets) > MaxWidgets {
		return fmt.Errorf("a dashboard holds at most %d widgets", MaxWidgets)
	}
	seen := map[string]bool{}
	for i := range d.Widgets {
		w := &d.Widgets[i]
		w.ID = strings.TrimSpace(w.ID)
		if w.ID == "" || len(w.ID) > 40 || seen[w.ID] {
			return errors.New("each widget needs a unique id of at most 40 characters")
		}
		seen[w.ID] = true
		w.Title = strings.TrimSpace(w.Title)
		if utf8.RuneCountInString(w.Title) > 120 {
			return errors.New("widget title must be at most 120 characters")
		}
		switch w.Kind {
		case KindTrend, KindFunnel, KindMetric, KindTraces:
		default:
			return fmt.Errorf("unknown widget kind %q", w.Kind)
		}
		if w.Width != 2 {
			w.Width = 1
		}
		if len(w.Query) == 0 || string(w.Query) == "null" {
			w.Query = json.RawMessage("{}")
		}
		if len(w.Query) > MaxWidgetBytes {
			return errors.New("widget query is too large")
		}
		if check != nil {
			if err := check(w.Kind, w.Query); err != nil {
				label := w.Title
				if label == "" {
					label = w.Kind
				}
				return fmt.Errorf("widget %q: %w", label, err)
			}
		}
	}
	if d.Widgets == nil {
		d.Widgets = []Widget{}
	}
	return nil
}

// Store keeps dashboards in Postgres.
type Store struct{ DB *pgxpool.Pool }

const cols = `d.id, d.project_id, d.name, d.description, COALESCE(d.created_by, 0), COALESCE(NULLIF(u.name, ''), u.email, ''),
	d.shared, d.widgets, d.created_at, d.updated_at`

func scan(row pgx.Row) (Dashboard, error) {
	var d Dashboard
	var raw []byte
	err := row.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Description, &d.CreatedBy, &d.CreatorName, &d.Shared, &raw, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
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
func (s *Store) List(ctx context.Context, project, user int64) ([]Dashboard, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+cols+` FROM dashboards d LEFT JOIN users u ON u.id = d.created_by
		WHERE d.project_id = $1 AND (d.shared OR d.created_by = $2) ORDER BY d.updated_at DESC, d.id DESC LIMIT 200`, project, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Dashboard{}
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
func (s *Store) Get(ctx context.Context, project, id, user int64) (Dashboard, error) {
	return scan(s.DB.QueryRow(ctx, `SELECT `+cols+` FROM dashboards d LEFT JOIN users u ON u.id = d.created_by
		WHERE d.id = $1 AND d.project_id = $2 AND (d.shared OR d.created_by = $3)`, id, project, user))
}

// Create saves a new dashboard owned by d.CreatedBy.
func (s *Store) Create(ctx context.Context, d Dashboard) (Dashboard, error) {
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
func (s *Store) Update(ctx context.Context, d Dashboard, user int64) (Dashboard, error) {
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
		return d, ErrNotFound
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
		return ErrNotFound
	}
	return nil
}

// CanEdit reports whether a user may change a dashboard: its creator, or a
// project owner when the dashboard is shared.
func CanEdit(d Dashboard, user int64, projectOwner bool) bool {
	return d.CreatedBy == user || (d.Shared && projectOwner)
}
