// Package postgres stores issues in Postgres.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/issues/domain"

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

func scan(row pgx.Row) (domain.Issue, error) {
	var i domain.Issue
	var status string
	err := row.Scan(&i.ID, &i.ProjectID, &i.Fingerprint, &i.Title, &i.ExceptionType, &i.Culprit, &i.Platform,
		&i.ServiceName, &status, &i.FirstSeen, &i.LastSeen, &i.Count, &i.SampleTraceID, &i.SampleSpanID,
		&i.ResolvedAt, &i.TimesRegressed)
	i.Status = domain.Status(status)
	return i, err
}

// Record adds an occurrence to its issue, creating or reopening it as needed.
// A new issue or regression also queues its event in the outbox, atomically.
// It runs in one transaction with a row lock, so concurrent processors agree
// on whether an occurrence was the first or a regression.
func (s *Store) Record(ctx context.Context, o domain.Occurrence) (domain.Issue, domain.Kind, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return domain.Issue{}, domain.KindNone, err
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
		return domain.Issue{}, domain.KindNone, err
	}
	created := tag.RowsAffected() == 1

	cur, err := scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM issues WHERE project_id = $1 AND fingerprint = $2 FOR UPDATE`,
		o.ProjectID, o.Fingerprint))
	if err != nil {
		return domain.Issue{}, domain.KindNone, err
	}

	next, kind := domain.Transition(!created, cur.Status, cur.ResolvedAt, o.Timestamp)
	regressed := kind == domain.KindRegression
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
		return domain.Issue{}, domain.KindNone, err
	}
	if kind != domain.KindNone {
		ev, err := json.Marshal(domain.NewEvent(updated, kind, o))
		if err != nil {
			return domain.Issue{}, domain.KindNone, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO issue_outbox (issue_id, payload) VALUES ($1, $2)`, updated.ID, ev); err != nil {
			return domain.Issue{}, domain.KindNone, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Issue{}, domain.KindNone, err
	}
	return updated, kind, nil
}

func (s *Store) List(ctx context.Context, f domain.ListFilter) ([]domain.Issue, int64, error) {
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
	out := []domain.Issue{}
	for rows.Next() {
		i, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, i)
	}
	return out, total, rows.Err()
}

func (s *Store) Get(ctx context.Context, id int64) (domain.Issue, error) {
	i, err := scan(s.db.QueryRow(ctx, "SELECT "+cols+" FROM issues WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return i, domain.ErrNotFound
	}
	return i, err
}

// SetStatus changes the status. Resolving stamps resolved_at so that only
// later events count as regressions.
func (s *Store) SetStatus(ctx context.Context, id int64, st domain.Status) (domain.Issue, error) {
	i, err := scan(s.db.QueryRow(ctx, `
		UPDATE issues SET status = $2::text,
			resolved_at = CASE WHEN $2::text = 'resolved' THEN now() ELSE NULL END
		WHERE id = $1 RETURNING `+cols, id, string(st)))
	if errors.Is(err, pgx.ErrNoRows) {
		return i, domain.ErrNotFound
	}
	return i, err
}

// Pool exposes the connection pool for stores that share it.
func (s *Store) Pool() *pgxpool.Pool { return s.db }

// New returns a Store on an existing pool.
func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

// RelayOutbox sends up to limit unsent outbox events in order with send and
// marks them sent, all in one transaction with the rows locked, so several
// relays never send the same row concurrently. A send error stops the batch;
// the remaining rows stay queued. It returns how many rows were sent.
func (s *Store) RelayOutbox(ctx context.Context, limit int, send func(ctx context.Context, issueID int64, payload []byte) error) (int, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT id, issue_id, payload FROM issue_outbox WHERE sent_at IS NULL
		ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return 0, err
	}
	type row struct {
		id, issue int64
		payload   []byte
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.issue, &r.payload); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var sent []int64
	var sendErr error
	for _, r := range batch {
		if sendErr = send(ctx, r.issue, r.payload); sendErr != nil {
			break
		}
		sent = append(sent, r.id)
	}
	if len(sent) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE issue_outbox SET sent_at = now() WHERE id = ANY($1)`, sent); err != nil {
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
	}
	return len(sent), sendErr
}

// PruneOutbox deletes events sent before cutoff.
func (s *Store) PruneOutbox(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, `DELETE FROM issue_outbox WHERE sent_at IS NOT NULL AND sent_at < $1`, cutoff)
	return tag.RowsAffected(), err
}
