package alerts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned for rules or channels outside the project.
var ErrNotFound = errors.New("not found")

// Entry is one row of a rule's history.
type Entry struct {
	ID         int64
	RuleID     int64
	At         time.Time
	State      State
	Previous   State
	Value      *float64
	Notice     Notice
	Suppressed string
	Failures   string
	IssueID    *int64
	TraceID    string
	Message    string
}

// Store keeps rules, channels and history in Postgres.
type Store struct{ DB *pgxpool.Pool }

const ruleCols = `id, project_id, name, metric, comparison, threshold, window_seconds, interval_seconds,
	service, route, environment, event_name, log_query, renotify_seconds, channel_ids, enabled, muted_until,
	state, state_since, last_value, last_evaluated_at, last_notified_at, last_triggered_at, created_at, updated_at`

func scanRule(row pgx.Row) (Rule, error) {
	var r Rule
	var window, interval, renotify int32
	var metric, cmp, state string
	err := row.Scan(&r.ID, &r.ProjectID, &r.Name, &metric, &cmp, &r.Threshold, &window, &interval,
		&r.Service, &r.Route, &r.Environment, &r.EventName, &r.LogQuery, &renotify, &r.ChannelIDs, &r.Enabled, &r.MutedUntil,
		&state, &r.StateSince, &r.LastValue, &r.LastEvaluatedAt, &r.LastNotifiedAt, &r.LastTriggeredAt, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	r.Metric, r.Comparison, r.State = Metric(metric), Comparison(cmp), State(state)
	r.Window = time.Duration(window) * time.Second
	r.Interval = time.Duration(interval) * time.Second
	r.Renotify = time.Duration(renotify) * time.Second
	if r.ChannelIDs == nil {
		r.ChannelIDs = []int64{}
	}
	return r, err
}

func collectRules(rows pgx.Rows, err error) ([]Rule, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListRules(ctx context.Context, project int64) ([]Rule, error) {
	return collectRules(s.DB.Query(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE project_id = $1 ORDER BY
		CASE state WHEN 'alerting' THEN 0 WHEN 'no_data' THEN 1 ELSE 2 END, name, id`, project))
}

func (s *Store) GetRule(ctx context.Context, project, id int64) (Rule, error) {
	return scanRule(s.DB.QueryRow(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE project_id = $1 AND id = $2`, project, id))
}

// checkChannels keeps only channel ids that belong to the project.
func (s *Store) checkChannels(ctx context.Context, project int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT id FROM alert_channels WHERE project_id = $1 AND id = ANY($2) ORDER BY id`, project, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func (s *Store) CreateRule(ctx context.Context, r Rule) (Rule, error) {
	ids, err := s.checkChannels(ctx, r.ProjectID, r.ChannelIDs)
	if err != nil {
		return Rule{}, err
	}
	return scanRule(s.DB.QueryRow(ctx, `INSERT INTO alert_rules (project_id, name, metric, comparison, threshold,
			window_seconds, interval_seconds, service, route, environment, event_name, renotify_seconds, channel_ids, enabled, log_query)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING `+ruleCols,
		r.ProjectID, r.Name, string(r.Metric), string(r.Comparison), r.Threshold, int(r.Window/time.Second), int(r.Interval/time.Second),
		r.Service, r.Route, r.Environment, r.EventName, int(r.Renotify/time.Second), ids, r.Enabled, r.LogQuery))
}

// UpdateRule saves the rule's settings. Changing what is measured resets the
// state, since the old state no longer describes the new condition.
func (s *Store) UpdateRule(ctx context.Context, r Rule) (Rule, error) {
	ids, err := s.checkChannels(ctx, r.ProjectID, r.ChannelIDs)
	if err != nil {
		return Rule{}, err
	}
	return scanRule(s.DB.QueryRow(ctx, `UPDATE alert_rules SET name = $3, metric = $4, comparison = $5, threshold = $6,
			window_seconds = $7, interval_seconds = $8, service = $9, route = $10, environment = $11, event_name = $12,
			renotify_seconds = $13, channel_ids = $14, enabled = $15, log_query = $16, updated_at = now(),
			last_evaluated_at = NULL,
			state = CASE WHEN metric <> $4 OR comparison <> $5 OR threshold <> $6 OR window_seconds <> $7 OR service <> $9
				OR route <> $10 OR environment <> $11 OR event_name <> $12 OR log_query <> $16 OR NOT $15 THEN 'ok' ELSE state END,
			state_since = CASE WHEN metric <> $4 OR comparison <> $5 OR threshold <> $6 OR window_seconds <> $7 OR service <> $9
				OR route <> $10 OR environment <> $11 OR event_name <> $12 OR log_query <> $16 OR NOT $15 THEN now() ELSE state_since END
		WHERE project_id = $1 AND id = $2 RETURNING `+ruleCols,
		r.ProjectID, r.ID, r.Name, string(r.Metric), string(r.Comparison), r.Threshold, int(r.Window/time.Second), int(r.Interval/time.Second),
		r.Service, r.Route, r.Environment, r.EventName, int(r.Renotify/time.Second), ids, r.Enabled, r.LogQuery))
}

func (s *Store) DeleteRule(ctx context.Context, project, id int64) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM alert_rules WHERE project_id = $1 AND id = $2`, project, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Mute pauses notifications until the given time; nil unmutes.
func (s *Store) Mute(ctx context.Context, project, id int64, until *time.Time) (Rule, error) {
	return scanRule(s.DB.QueryRow(ctx, `UPDATE alert_rules SET muted_until = $3, updated_at = now()
		WHERE project_id = $1 AND id = $2 RETURNING `+ruleCols, project, id, until))
}

// Rules returns enabled rules across projects, for the worker.
func (s *Store) Rules(ctx context.Context) ([]Rule, error) {
	return collectRules(s.DB.Query(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE enabled ORDER BY id`))
}

// EventRules returns enabled event-driven rules of a project for a metric.
func (s *Store) EventRules(ctx context.Context, project int64, m Metric) ([]Rule, error) {
	return collectRules(s.DB.Query(ctx, `SELECT `+ruleCols+` FROM alert_rules WHERE enabled AND project_id = $1 AND metric = $2 ORDER BY id`, project, string(m)))
}

// Record saves an evaluation: the rule's new state and, when something
// happened worth keeping, a history entry. Notified marks a sent notice.
func (s *Store) Record(ctx context.Context, r Rule, out Outcome, value *float64, e Entry, notified, triggered bool, now time.Time) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE alert_rules SET
			state = $2,
			state_since = CASE WHEN state <> $2 THEN $3 ELSE state_since END,
			last_value = $4,
			last_evaluated_at = $3,
			last_notified_at = CASE WHEN $5 THEN $3 ELSE last_notified_at END,
			last_triggered_at = CASE WHEN $6 THEN $3 ELSE last_triggered_at END
		WHERE id = $1`, r.ID, string(out.State), now, value, notified, triggered)
	if err != nil {
		return err
	}
	if out.Changed || out.Notice != NoticeNone {
		_, err = tx.Exec(ctx, `INSERT INTO alert_history (rule_id, at, state, previous, value, notice, suppressed, failures, issue_id, trace_id, message)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			r.ID, now, string(out.State), string(e.Previous), value, string(out.Notice), out.Suppressed, e.Failures,
			e.IssueID, e.TraceID, e.Message)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) History(ctx context.Context, rule int64, limit int) ([]Entry, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, rule_id, at, state, previous, value, notice, suppressed, failures, issue_id, trace_id, message
		FROM alert_history WHERE rule_id = $1 ORDER BY at DESC, id DESC LIMIT $2`, rule, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var st, prev, notice string
		if err := rows.Scan(&e.ID, &e.RuleID, &e.At, &st, &prev, &e.Value, &notice, &e.Suppressed, &e.Failures, &e.IssueID, &e.TraceID, &e.Message); err != nil {
			return nil, err
		}
		e.State, e.Previous, e.Notice = State(st), State(prev), Notice(notice)
		out = append(out, e)
	}
	return out, rows.Err()
}

const channelCols = `id, project_id, name, kind, target, secret, created_at`

func scanChannel(row pgx.Row) (Channel, error) {
	var c Channel
	var kind string
	err := row.Scan(&c.ID, &c.ProjectID, &c.Name, &kind, &c.Target, &c.Secret, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	c.Kind = ChannelKind(kind)
	return c, err
}

func (s *Store) ListChannels(ctx context.Context, project int64) ([]Channel, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+channelCols+` FROM alert_channels WHERE project_id = $1 ORDER BY id`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) GetChannel(ctx context.Context, project, id int64) (Channel, error) {
	return scanChannel(s.DB.QueryRow(ctx, `SELECT `+channelCols+` FROM alert_channels WHERE project_id = $1 AND id = $2`, project, id))
}

// Channels loads a rule's channels.
func (s *Store) Channels(ctx context.Context, project int64, ids []int64) ([]Channel, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.DB.Query(ctx, `SELECT `+channelCols+` FROM alert_channels WHERE project_id = $1 AND id = ANY($2) ORDER BY id`, project, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// NewSecret returns a random webhook signing secret.
func NewSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "whsec_" + hex.EncodeToString(b)
}

func (s *Store) CreateChannel(ctx context.Context, c Channel) (Channel, error) {
	if c.Kind == Webhook && c.Secret == "" {
		c.Secret = NewSecret()
	}
	return scanChannel(s.DB.QueryRow(ctx, `INSERT INTO alert_channels (project_id, name, kind, target, secret)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+channelCols, c.ProjectID, c.Name, string(c.Kind), c.Target, c.Secret))
}

// DeleteChannel removes the channel and drops it from the project's rules.
func (s *Store) DeleteChannel(ctx context.Context, project, id int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM alert_channels WHERE project_id = $1 AND id = $2`, project, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE alert_rules SET channel_ids = array_remove(channel_ids, $2) WHERE project_id = $1`, project, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
