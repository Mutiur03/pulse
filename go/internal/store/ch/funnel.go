package ch

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FunnelQuery is a validated funnel definition.
type FunnelQuery struct {
	Project       uint64
	Steps         []string
	WindowSeconds int
	From, To      time.Time
}

// Validate checks the step count and window, which are interpolated as
// windowFunnel parameters (ClickHouse requires literals there).
func (f FunnelQuery) Validate() error {
	if len(f.Steps) < 2 || len(f.Steps) > 5 {
		return fmt.Errorf("a funnel needs 2 to 5 steps")
	}
	for _, s := range f.Steps {
		if s == "" {
			return fmt.Errorf("step event names must not be empty")
		}
	}
	if f.WindowSeconds < 1 || f.WindowSeconds > 30*24*3600 {
		return fmt.Errorf("window must be between 1 second and 30 days")
	}
	if !f.To.After(f.From) {
		return fmt.Errorf("time range is empty")
	}
	return nil
}

// funnelEvents selects events in the funnel's range with the person each
// belongs to: ids linked by identify or alias count as one user. The range
// filter is applied before the alias join, so the join only sees
// the rows it needs.
func funnelEvents(f FunnelQuery) (string, []interface{}) {
	args := []interface{}{f.Project, f.From, f.To}
	where := "project_id = ? AND timestamp >= ? AND timestamp < ? AND distinct_id != ''"
	args = append(args, f.Project)
	return `(SELECT if(a.person_key != '', a.person_key, e.distinct_id) AS person,
			e.timestamp AS timestamp, e.event AS event, e.session_id AS session_id
		FROM (SELECT distinct_id, timestamp, event, session_id FROM events WHERE ` + where + `) AS e
		LEFT JOIN (SELECT distinct_id, argMax(person_key, updated_at) AS person_key FROM person_aliases
			WHERE project_id = ? GROUP BY distinct_id) AS a ON a.distinct_id = e.distinct_id)`, args
}

// levelExpr is the furthest step (0..n) a person reached in order within
// the window. The step count and window are validated literals.
func levelExpr(f FunnelQuery) (string, []interface{}) {
	conds := make([]string, len(f.Steps))
	args := []interface{}{}
	for i, s := range f.Steps {
		conds[i] = "event = ?"
		args = append(args, s)
	}
	return fmt.Sprintf("windowFunnel(%d)(toDateTime(timestamp), %s)", f.WindowSeconds, strings.Join(conds, ", ")), args
}

// errorSessionsSQL and latencySessionsSQL select the sessions in range
// with errors, and with backend server spans. Both are small next to the
// events, so they are used as IN sets while events are scanned.
const errorSessionsSQL = `SELECT session_id FROM errors
	WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND session_id != ''`

const latencySpansWhere = `project_id = ? AND timestamp >= ? AND timestamp < ?
	AND span_kind = 'server' AND platform != 'webjs' AND session_id != ''`

// errorCohortSQL selects one row per (step s, user) for users who reached
// s with s < n, kind 'dropped' if they stopped at s, else 'continued', with
// the user's sessions that had errors.
func errorCohortSQL(f FunnelQuery) (string, []interface{}) {
	level, args := levelExpr(f)
	from, fargs := funnelEvents(f)
	args = append(args, f.Project, f.From, f.To)
	args = append(args, fargs...)
	return fmt.Sprintf(`SELECT person AS distinct_id, step, if(level = step, 'dropped', 'continued') AS kind, error_sessions
		FROM (
			SELECT person, %s AS level,
				groupUniqArrayIf(session_id, session_id IN (`+errorSessionsSQL+`)) AS error_sessions
			FROM %s GROUP BY person HAVING level >= 1
		)
		ARRAY JOIN range(1, %d) AS step
		WHERE level >= step`, level, from, len(f.Steps)), args
}

// FunnelHealthSQL computes, in one scan of the events, the number of users
// by furthest step reached (rows with kind 'level', step = level) and, per
// step and cohort, users, sessions, sessions with errors and the p95
// duration of backend server spans. Users are aggregated per level first; a
// user at level L belongs to the 'continued' cohort of steps 1..L-1 and the
// 'dropped' cohort of step L (when L < n). Each user has one level, so the
// per-level states merge into cohorts without double counting.
func FunnelHealthSQL(f FunnelQuery) (string, []interface{}) {
	level, args := levelExpr(f)
	from, fargs := funnelEvents(f)
	// In the order the placeholders appear: level conditions, the error and
	// latency session sets, the events, the step list, the latency spans.
	args = append(args, f.Project, f.From, f.To, f.Project, f.From, f.To)
	args = append(args, fargs...)
	args = append(args, f.Steps, f.Project, f.From, f.To)
	q := fmt.Sprintf(`SELECT toUInt8(c.1) AS step, c.2 AS kind, sum(users) AS users,
		uniqMerge(sessions_state) AS sessions, uniqMerge(errors_state) AS error_sessions,
		if(isNaN(quantileMerge(0.95)(lat_state)), 0, quantileMerge(0.95)(lat_state)) AS p95_ns
	FROM (
		SELECT level, uniqExact(person) AS users, uniqMergeState(sessions) AS sessions_state,
			uniqMergeState(error_sessions) AS errors_state, quantileMergeState(0.95)(l.lat) AS lat_state
		FROM (
			SELECT person, level, sessions, error_sessions, latency_session
			FROM (
				SELECT person, %s AS level,
					uniqStateIf(session_id, session_id != '') AS sessions,
					uniqStateIf(session_id, session_id IN (`+errorSessionsSQL+`)) AS error_sessions,
					groupUniqArrayIf(session_id, session_id IN (SELECT session_id FROM spans WHERE `+latencySpansWhere+`)) AS latency_sessions
				FROM %s GROUP BY person HAVING countIf(event IN (?)) > 0
			)
			LEFT ARRAY JOIN latency_sessions AS latency_session
		) AS p
		LEFT JOIN (
			SELECT session_id, quantileState(0.95)(duration_ns) AS lat FROM spans
			WHERE `+latencySpansWhere+`
			GROUP BY session_id
		) AS l ON p.latency_session = l.session_id
		GROUP BY level
	)
	ARRAY JOIN arrayConcat([(toUInt64(level), 'level')],
		arrayMap(x -> (x, if(x = level, 'dropped', 'continued')), range(1, least(toUInt64(level), %d) + 1))) AS c
	GROUP BY step, kind
	ORDER BY step, kind`, level, from, len(f.Steps)-1)
	return q, args
}

// FunnelErrorsSQL lists the top issues per step and cohort, ranked by the
// number of affected users. Only sessions with errors are expanded.
func FunnelErrorsSQL(f FunnelQuery, perCohort int) (string, []interface{}) {
	cohort, args := errorCohortSQL(f)
	q := `SELECT c.step AS step, c.kind AS kind, errs.issue_id AS issue_id, any(errs.title) AS title,
		any(errs.exception_type) AS exception_type, any(errs.service_name) AS service_name,
		uniqExact(c.distinct_id) AS users, count() AS n
	FROM (
		SELECT distinct_id, step, kind, session_id
		FROM (` + cohort + `)
		ARRAY JOIN error_sessions AS session_id
	) AS c
	INNER JOIN (
		SELECT session_id, issue_id, title, exception_type, service_name FROM errors
		WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND issue_id != 0 AND session_id != ''
	) AS errs ON c.session_id = errs.session_id
	GROUP BY step, kind, issue_id
	ORDER BY step, kind, users DESC, n DESC
	LIMIT ? BY step, kind`
	args = append(args, f.Project, f.From, f.To, perCohort)
	return q, args
}

// FunnelLevel is the number of users whose furthest step is Level.
type FunnelLevel struct {
	Level uint8  `ch:"level"`
	Users uint64 `ch:"users"`
}

// FunnelHealth is backend health for one step's cohort.
type FunnelHealth struct {
	Step          uint8   `ch:"step"`
	Kind          string  `ch:"kind"`
	Users         uint64  `ch:"users"`
	Sessions      uint64  `ch:"sessions"`
	ErrorSessions uint64  `ch:"error_sessions"`
	P95Ns         float64 `ch:"p95_ns"`
}

// FunnelError is an issue seen in one step's cohort.
type FunnelError struct {
	Step          uint8  `ch:"step"`
	Kind          string `ch:"kind"`
	IssueID       uint64 `ch:"issue_id"`
	Title         string `ch:"title"`
	ExceptionType string `ch:"exception_type"`
	ServiceName   string `ch:"service_name"`
	Users         uint64 `ch:"users"`
	Count         uint64 `ch:"n"`
}

// FunnelData is the raw result of a funnel run.
type FunnelData struct {
	Levels []FunnelLevel
	Health []FunnelHealth
	Errors []FunnelError
}

func (c *Client) RunFunnel(ctx context.Context, f FunnelQuery) (FunnelData, error) {
	var d FunnelData
	if err := f.Validate(); err != nil {
		return d, err
	}
	// The two reads are independent; run them at once and stop the other
	// when one fails.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	run := func(name string, dest interface{}, q string, args []interface{}) {
		if err := c.conn.Select(ctx, dest, q, args...); err != nil {
			errs <- fmt.Errorf("funnel %s: %w", name, err)
			return
		}
		errs <- nil
	}
	f.From, f.To = cacheRange(f.From, f.To)
	ctx = cached(ctx, true)
	var health []FunnelHealth
	q, args := FunnelHealthSQL(f)
	go run("health", &health, q, args)
	go func() {
		// Most ranges have no errors tied to sessions; skip the scan then.
		var n []struct {
			N uint64 `ch:"n"`
		}
		if err := c.conn.Select(ctx, &n, `SELECT count() AS n FROM errors
			WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND issue_id != 0 AND session_id != ''`,
			f.Project, f.From, f.To); err != nil {
			errs <- fmt.Errorf("funnel errors: %w", err)
			return
		}
		if len(n) == 0 || n[0].N == 0 {
			errs <- nil
			return
		}
		q2, args2 := FunnelErrorsSQL(f, 5)
		run("errors", &d.Errors, q2, args2)
	}()
	var first error
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil && first == nil {
			first = err
			cancel()
		}
	}
	if first != nil {
		return d, first
	}
	d.Levels, d.Health = splitFunnelHealth(health)
	return d, nil
}

// splitFunnelHealth separates the level rows of FunnelHealthSQL from the
// cohort rows.
func splitFunnelHealth(rows []FunnelHealth) ([]FunnelLevel, []FunnelHealth) {
	levels := []FunnelLevel{}
	health := []FunnelHealth{}
	for _, r := range rows {
		if r.Kind == "level" {
			levels = append(levels, FunnelLevel{Level: r.Step, Users: r.Users})
		} else {
			health = append(health, r)
		}
	}
	return levels, health
}
