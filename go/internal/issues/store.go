package issues

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists issues in Postgres.
type Store struct{ db *pgxpool.Pool }

func Connect(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return &Store{db: pool}, nil
}

func (s *Store) Close() { s.db.Close() }

const cols = `id, project_id, fingerprint, title, exception_type, culprit, platform, service_name, status,
	first_seen, last_seen, count, sample_trace_id, sample_span_id, resolved_at, times_regressed`

func scan(row pgx.Row) (Issue, error) {
	var i Issue
	var status string
	err := row.Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.ExceptionType, &i.Culprit, &i.Platform,
		&i.ServiceName, &status, &i.FirstSeen, &i.LastSeen, &i.Count, &i.SampleTraceID, &i.SampleSpanID,
		&i.ResolvedAt, &i.TimesRegressed)
	i.Status = Status(status)
	return i, err
}

// Record adds an occurrence to its issue, creating or reopening it as needed.
// It runs in one transaction with a row lock, so concurrent processors agree
// on whether an occurrence was the first or a regression.
func (s *Store) Record(ctx context.Context, o Occurrence) (Issue, Kind, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Issue{}, KindNone, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Make sure the row exists without overwriting anything, then lock it.
	tag, err := tx.Exec(ctx, `
		INSERT INTO issues (project_id, fingerprint, title, exception_type, culprit, platform, service_name,
			status, first_seen, last_seen, count, sample_trace_id, sample_span_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'unresolved', $8, $8, 0, $9, $10)
		ON CONFLICT (project_id, fingerprint) DO NOTHING`,
		o.ProjectID, o.Fingerprint, o.Title, o.ExceptionType, o.Culprit, o.Platform, o.ServiceName,
		o.Timestamp, o.TraceID, o.SpanID)
	if err != nil {
		return Issue{}, KindNone, err
	}
	created := tag.RowsAffected() == 1

	cur, err := scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM issues WHERE project_id = $1 AND fingerprint = $2 FOR UPDATE`,
		o.ProjectID, o.Fingerprint))
	if err != nil {
		return Issue{}, KindNone, err
	}

	next, kind := Transition(!created, cur.Status, cur.ResolvedAt, o.Timestamp)
	regressed := kind == KindRegression
	updated, err := scan(tx.QueryRow(ctx, `
		UPDATE issues SET
			count = count + 1,
			last_seen = GREATEST(last_seen, $2::timestamptz),
			first_seen = LEAST(first_seen, $2::timestamptz),
			status = $3::text,
			resolved_at = CASE WHEN $4::boolean THEN NULL ELSE resolved_at END,
			times_regressed = times_regressed + CASE WHEN $4::boolean THEN 1 ELSE 0 END,
			last_regressed = CASE WHEN $4::boolean THEN $2::timestamptz ELSE last_regressed END,
			sample_trace_id = CASE WHEN $5::text <> '' THEN $5::text ELSE sample_trace_id END,
			sample_span_id = CASE WHEN $5::text <> '' THEN $6::text ELSE sample_span_id END
		WHERE id = $1
		RETURNING `+cols, cur.ID, o.Timestamp, string(next), regressed, o.TraceID, o.SpanID))
	if err != nil {
		return Issue{}, KindNone, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Issue{}, KindNone, err
	}
	return updated, kind, nil
}

// ListFilter selects issues for the list endpoint.
type ListFilter struct {
	ProjectID int64
	Status    Status
	Service   string
	Query     string
	Sort      string // last_seen (default), first_seen, count
	Limit     int
	Offset    int
}

func (s *Store) List(ctx context.Context, f ListFilter) ([]Issue, int64, error) {
	where := []string{"project_id = $1"}
	args := []interface{}{f.ProjectID}
	if f.Status != "" {
		args = append(args, string(f.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if f.Service != "" {
		args = append(args, f.Service)
		where = append(where, fmt.Sprintf("service_name = $%d", len(args)))
	}
	if f.Query != "" {
		args = append(args, "%"+f.Query+"%")
		where = append(where, fmt.Sprintf("(title ILIKE $%d OR culprit ILIKE $%d)", len(args), len(args)))
	}
	order := map[string]string{"first_seen": "first_seen DESC", "count": "count DESC"}[f.Sort]
	if order == "" {
		order = "last_seen DESC"
	}
	w := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRow(ctx, "SELECT count(*) FROM issues WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit, f.Offset)
	rows, err := s.db.Query(ctx, fmt.Sprintf("SELECT %s FROM issues WHERE %s ORDER BY %s, id DESC LIMIT $%d OFFSET $%d",
		cols, w, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Issue{}
	for rows.Next() {
		i, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, i)
	}
	return out, total, rows.Err()
}

var ErrNotFound = errors.New("issue not found")

func (s *Store) Get(ctx context.Context, id int64) (Issue, error) {
	i, err := scan(s.db.QueryRow(ctx, "SELECT "+cols+" FROM issues WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return i, ErrNotFound
	}
	return i, err
}

// SetStatus changes the status. Resolving stamps resolved_at so that only
// later events count as regressions.
func (s *Store) SetStatus(ctx context.Context, id int64, st Status) (Issue, error) {
	i, err := scan(s.db.QueryRow(ctx, `
		UPDATE issues SET status = $2::text,
			resolved_at = CASE WHEN $2::text = 'resolved' THEN now() ELSE NULL END
		WHERE id = $1 RETURNING `+cols, id, string(st)))
	if errors.Is(err, pgx.ErrNoRows) {
		return i, ErrNotFound
	}
	return i, err
}
