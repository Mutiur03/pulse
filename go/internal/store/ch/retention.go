package ch

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RetentionQuery groups persons into cohorts by the period they first did
// StartEvent and counts how many did ReturnEvent in each later period. An
// empty event means any event.
type RetentionQuery struct {
	Project     uint64
	StartEvent  string
	ReturnEvent string
	Period      string // day or week
	Periods     int    // offsets 0..Periods
	From, To    time.Time
}

// MaxRetentionPeriods bounds the matrix width.
const MaxRetentionPeriods = 30

// Validate checks the query and fills in defaults.
func (q *RetentionQuery) Validate() error {
	if q.Period == "" {
		q.Period = "day"
	}
	if q.Period != "day" && q.Period != "week" {
		return errors.New("period must be day or week")
	}
	if q.Periods == 0 {
		q.Periods = 7
	}
	if q.Periods < 1 || q.Periods > MaxRetentionPeriods {
		return fmt.Errorf("periods must be 1-%d", MaxRetentionPeriods)
	}
	if len(q.StartEvent) > 200 || len(q.ReturnEvent) > 200 {
		return errors.New("event name is too long")
	}
	if !q.To.After(q.From) {
		return errors.New("from must be before to")
	}
	if q.To.Sub(q.From) > 400*24*time.Hour {
		return errors.New("range is at most 400 days")
	}
	return nil
}

func (q RetentionQuery) trunc(col string) string {
	if q.Period == "week" {
		return "toDateTime(toMonday(" + col + "), 'UTC')"
	}
	return "toStartOfDay(" + col + ")"
}

// RetentionSQL returns the cohort sizes query and the returns query. A
// person's cohort is the period of their first StartEvent in the range.
func RetentionSQL(q RetentionQuery) (sizes string, sizeArgs []interface{}, returns string, returnArgs []interface{}) {
	cohort := `SELECT person, ` + q.trunc("min(timestamp)") + ` AS cohort FROM ` + personEvents + ` WHERE (? = '' OR event = ?) GROUP BY person`
	cargs := append(personArgs(q.Project, q.From, q.To), q.StartEvent, q.StartEvent)
	sizes = `SELECT cohort, count() AS users FROM (` + cohort + `) GROUP BY cohort ORDER BY cohort`
	act := `SELECT DISTINCT person, ` + q.trunc("timestamp") + ` AS active FROM ` + personEvents + ` WHERE (? = '' OR event = ?)`
	aargs := append(personArgs(q.Project, q.From, q.To), q.ReturnEvent, q.ReturnEvent)
	returns = `SELECT cohort, toUInt32(dateDiff('` + q.Period + `', cohort, active)) AS offset, uniqExact(person) AS users
	FROM (` + cohort + `) AS c INNER JOIN (` + act + `) AS a USING person
	WHERE active >= cohort AND dateDiff('` + q.Period + `', cohort, active) <= ?
	GROUP BY cohort, offset ORDER BY cohort, offset`
	returnArgs = append(append(append([]interface{}{}, cargs...), aargs...), q.Periods)
	return sizes, cargs, returns, returnArgs
}

// RetentionCell is the users of one cohort at one offset.
type RetentionCell struct {
	Cohort time.Time `ch:"cohort"`
	Offset uint32    `ch:"offset"`
	Users  uint64    `ch:"users"`
}

// RetentionData holds cohort sizes and returning users.
type RetentionData struct {
	Sizes   []RetentionCell
	Returns []RetentionCell
}

// RunRetention runs both retention queries.
func (c *Client) RunRetention(ctx context.Context, q RetentionQuery) (RetentionData, error) {
	var d RetentionData
	s, sargs, r, rargs := RetentionSQL(q)
	var sizes []struct {
		Cohort time.Time `ch:"cohort"`
		Users  uint64    `ch:"users"`
	}
	if err := c.conn.Select(ctx, &sizes, s, sargs...); err != nil {
		return d, err
	}
	for _, x := range sizes {
		d.Sizes = append(d.Sizes, RetentionCell{Cohort: x.Cohort, Users: x.Users})
	}
	err := c.conn.Select(ctx, &d.Returns, r, rargs...)
	return d, err
}

// PathsQuery follows sessions step by step: through page paths ($pathname
// of $pageview) or through event names.
type PathsQuery struct {
	Project  uint64
	Mode     string // "pageview" or "event"
	Start    string // optional node to start from
	Steps    int
	From, To time.Time
}

// Limits on paths.
const (
	MaxPathSteps = 8
	MaxPathRows  = 2000
)

// Validate checks the query and fills in defaults.
func (q *PathsQuery) Validate() error {
	if q.Mode == "" {
		q.Mode = "pageview"
	}
	if q.Mode != "pageview" && q.Mode != "event" {
		return errors.New("mode must be pageview or event")
	}
	if q.Steps == 0 {
		q.Steps = 4
	}
	if q.Steps < 2 || q.Steps > MaxPathSteps {
		return fmt.Errorf("steps must be 2-%d", MaxPathSteps)
	}
	if len(q.Start) > 500 {
		return errors.New("start is too long")
	}
	if !q.To.After(q.From) {
		return errors.New("from must be before to")
	}
	return nil
}

// PathsSQL returns each distinct path (consecutive repeats collapsed,
// starting at Start when set, cut to Steps+1 nodes so callers can tell
// sessions that went on from those that ended) with its session count.
func PathsSQL(q PathsQuery) (string, []interface{}) {
	node, filter := "JSONExtractString(properties, '$pathname')", "event = '$pageview'"
	if q.Mode == "event" {
		node, filter = "event", "event NOT IN ('$web_vitals', '$autocapture', '$identify', '$create_alias')"
	}
	sql := `SELECT arraySlice(seq, if(? = '', 1, indexOf(seq, ?)), ?) AS path, count() AS sessions
	FROM (SELECT session_id, arrayCompact(arrayMap(x -> x.2, arraySort(x -> x.1, groupArray((timestamp, node))))) AS seq
		FROM (SELECT session_id, timestamp, ` + node + ` AS node FROM events
			WHERE project_id = ? AND timestamp >= ? AND timestamp < ? AND session_id != '' AND ` + filter + `)
		WHERE node != '' GROUP BY session_id)
	WHERE ? = '' OR has(seq, ?)
	GROUP BY path ORDER BY sessions DESC, path LIMIT ` + fmt.Sprint(MaxPathRows)
	return sql, []interface{}{q.Start, q.Start, q.Steps + 1, q.Project, q.From, q.To, q.Start, q.Start}
}

// PathRow is one distinct path.
type PathRow struct {
	Path     []string `ch:"path"`
	Sessions uint64   `ch:"sessions"`
}

// RunPaths runs PathsSQL.
func (c *Client) RunPaths(ctx context.Context, q PathsQuery) ([]PathRow, error) {
	sql, args := PathsSQL(q)
	var out []PathRow
	err := c.conn.Select(ctx, &out, sql, args...)
	return out, err
}
