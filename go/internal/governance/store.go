package governance

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store keeps data settings and deletion requests in Postgres.
type Store struct{ DB *pgxpool.Pool }

const settingsCols = `project_id, scrub_emails, scrub_cards, scrub_ips, scrub_keys, scrub_patterns,
	retention_events_days, retention_traces_days, retention_logs_days, retention_errors_days,
	retention_replays_days, retention_sourcemaps_days, updated_at`

func scanSettings(row pgx.Row) (Settings, error) {
	var s Settings
	err := row.Scan(&s.ProjectID, &s.Rules.Emails, &s.Rules.Cards, &s.Rules.IPs, &s.Rules.Keys, &s.Rules.Patterns,
		&s.Retention.EventsDays, &s.Retention.TracesDays, &s.Retention.LogsDays, &s.Retention.ErrorsDays,
		&s.Retention.ReplaysDays, &s.Retention.SourceMapsDays, &s.UpdatedAt)
	s.Saved = err == nil
	return s, err
}

// Settings returns a project's settings, or the defaults.
func (st *Store) Settings(ctx context.Context, project int64) (Settings, error) {
	s, err := scanSettings(st.DB.QueryRow(ctx, `SELECT `+settingsCols+` FROM project_data_settings WHERE project_id = $1`, project))
	if errors.Is(err, pgx.ErrNoRows) {
		return Defaults(project), nil
	}
	return s, err
}

// SaveSettings validates and stores a project's settings.
func (st *Store) SaveSettings(ctx context.Context, s Settings) (Settings, error) {
	if err := s.Validate(); err != nil {
		return s, err
	}
	r, ret := s.Rules, s.Retention
	return scanSettings(st.DB.QueryRow(ctx, `INSERT INTO project_data_settings (project_id, scrub_emails, scrub_cards, scrub_ips,
			scrub_keys, scrub_patterns, retention_events_days, retention_traces_days, retention_logs_days, retention_errors_days,
			retention_replays_days, retention_sourcemaps_days, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
		ON CONFLICT (project_id) DO UPDATE SET scrub_emails = $2, scrub_cards = $3, scrub_ips = $4, scrub_keys = $5,
			scrub_patterns = $6, retention_events_days = $7, retention_traces_days = $8, retention_logs_days = $9,
			retention_errors_days = $10, retention_replays_days = $11, retention_sourcemaps_days = $12, updated_at = now()
		RETURNING `+settingsCols,
		s.ProjectID, r.Emails, r.Cards, r.IPs, r.Keys, r.Patterns, ret.EventsDays, ret.TracesDays, ret.LogsDays,
		ret.ErrorsDays, ret.ReplaysDays, ret.SourceMapsDays))
}

// AllSettings returns the settings of every project, defaults included.
func (st *Store) AllSettings(ctx context.Context) ([]Settings, error) {
	rows, err := st.DB.Query(ctx, `SELECT p.id, d.project_id IS NOT NULL, COALESCE(d.retention_events_days, 0),
			COALESCE(d.retention_traces_days, 0), COALESCE(d.retention_logs_days, 0), COALESCE(d.retention_errors_days, 0),
			COALESCE(d.retention_replays_days, 0), COALESCE(d.retention_sourcemaps_days, 0)
		FROM projects p LEFT JOIN project_data_settings d ON d.project_id = p.id ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Settings
	for rows.Next() {
		var id int64
		var saved bool
		var r Retention
		if err := rows.Scan(&id, &saved, &r.EventsDays, &r.TracesDays, &r.LogsDays, &r.ErrorsDays, &r.ReplaysDays, &r.SourceMapsDays); err != nil {
			return nil, err
		}
		s := Defaults(id)
		if saved {
			s.Retention, s.Saved = r, true
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Request statuses.
const (
	Pending   = "pending"
	Running   = "running"
	Completed = "completed"
	Failed    = "failed"
)

// Request is a deletion request for one person.
type Request struct {
	ID          int64
	ProjectID   int64
	DistinctID  string
	PersonKey   string
	DistinctIDs []string
	Reason      string
	Status      string
	RequestedBy *int64
	Result      map[string]int64
	Error       string
	Attempts    int
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

const requestCols = `id, project_id, distinct_id, person_key, distinct_ids, reason, status, requested_by, result,
	error, attempts, created_at, started_at, completed_at`

func scanRequest(row pgx.Row) (Request, error) {
	var r Request
	var result []byte
	err := row.Scan(&r.ID, &r.ProjectID, &r.DistinctID, &r.PersonKey, &r.DistinctIDs, &r.Reason, &r.Status, &r.RequestedBy,
		&result, &r.Error, &r.Attempts, &r.CreatedAt, &r.StartedAt, &r.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	r.Result = map[string]int64{}
	if len(result) > 0 {
		_ = json.Unmarshal(result, &r.Result)
	}
	return r, err
}

// CreateRequest queues a deletion.
func (st *Store) CreateRequest(ctx context.Context, r Request) (Request, error) {
	if r.DistinctIDs == nil {
		r.DistinctIDs = []string{}
	}
	return scanRequest(st.DB.QueryRow(ctx, `INSERT INTO deletion_requests (project_id, distinct_id, person_key, distinct_ids, reason, requested_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+requestCols, r.ProjectID, r.DistinctID, r.PersonKey, r.DistinctIDs, r.Reason, r.RequestedBy))
}

// ListRequests returns a project's requests, newest first.
func (st *Store) ListRequests(ctx context.Context, project int64, limit int) ([]Request, error) {
	rows, err := st.DB.Query(ctx, `SELECT `+requestCols+` FROM deletion_requests WHERE project_id = $1 ORDER BY id DESC LIMIT $2`, project, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRequest returns one request of a project.
func (st *Store) GetRequest(ctx context.Context, project, id int64) (Request, error) {
	return scanRequest(st.DB.QueryRow(ctx, `SELECT `+requestCols+` FROM deletion_requests WHERE project_id = $1 AND id = $2`, project, id))
}

// StaleAfter is when a running request is assumed abandoned and retried.
const StaleAfter = 15 * time.Minute

// MaxAttempts stops retrying a request that keeps failing.
const MaxAttempts = 5

// Claim takes the oldest pending request (or one abandoned while running).
// ok is false when there is none.
func (st *Store) Claim(ctx context.Context) (Request, bool, error) {
	r, err := scanRequest(st.DB.QueryRow(ctx, `UPDATE deletion_requests SET status = 'running', started_at = now(), attempts = attempts + 1
		WHERE id = (SELECT id FROM deletion_requests
			WHERE (status = 'pending' OR (status = 'running' AND started_at < now() - make_interval(secs => $1)))
				AND attempts < $2
			ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING `+requestCols, StaleAfter.Seconds(), MaxAttempts))
	if errors.Is(err, ErrNotFound) {
		return r, false, nil
	}
	return r, err == nil, err
}

// Finish records the outcome. A failure goes back to pending until
// MaxAttempts, then stays failed.
func (st *Store) Finish(ctx context.Context, id int64, ids []string, result map[string]int64, runErr error) (Request, error) {
	b, _ := json.Marshal(result)
	if ids == nil {
		ids = []string{}
	}
	if runErr == nil {
		return scanRequest(st.DB.QueryRow(ctx, `UPDATE deletion_requests SET status = 'completed', completed_at = now(), result = $2,
			distinct_ids = $3, error = '' WHERE id = $1 RETURNING `+requestCols, id, b, ids))
	}
	return scanRequest(st.DB.QueryRow(ctx, `UPDATE deletion_requests SET
			status = CASE WHEN attempts >= $3 THEN 'failed' ELSE 'pending' END,
			completed_at = CASE WHEN attempts >= $3 THEN now() END, error = $2
		WHERE id = $1 RETURNING `+requestCols, id, runErr.Error(), MaxAttempts))
}
