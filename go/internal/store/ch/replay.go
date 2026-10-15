package ch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/replay"
)

// InsertReplayChunk records one stored replay chunk.
func (c *Client) InsertReplayChunk(ctx context.Context, m replay.Meta) error {
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO replay_chunks (project_id, session_id, chunk_key, start, end, distinct_id, events, pages, errors, urls, trace_ids, bytes)")
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}
	if err := batch.Append(m.ProjectID, m.SessionID, m.Key, m.Start, m.End, m.DistinctID, m.Events, m.Pages, m.Errors,
		nonNil(m.URLs), nonNil(m.TraceIDs), m.Bytes); err != nil {
		return err
	}
	return batch.Send()
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ReplayFilter selects sessions for the replays list.
type ReplayFilter struct {
	Project       uint64
	From, To      time.Time
	HasErrors     bool
	DistinctID    string
	URL           string
	MinDurationMs int64
	SessionID     string
	Limit         int
}

// ReplaySummary is one recorded session.
type ReplaySummary struct {
	SessionID  string    `ch:"session_id"`
	DistinctID string    `ch:"user_id"`
	Start      time.Time `ch:"first_at"`
	End        time.Time `ch:"last_at"`
	DurationMs int64     `ch:"duration_ms"`
	Pages      uint64    `ch:"page_total"`
	Errors     uint64    `ch:"error_total"`
	Chunks     uint64    `ch:"chunks"`
	Bytes      uint64    `ch:"byte_total"`
	URLs       []string  `ch:"url_list"`
}

// ReplaysSQL builds the session summary query. Sessions overlapping the time
// range are included; filters on the whole session go in HAVING.
func ReplaysSQL(f ReplayFilter) (string, []interface{}) {
	where := []string{"project_id = ?", "start < ?", "end >= ?"}
	args := []interface{}{f.Project, f.To, f.From}
	if f.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, f.SessionID)
	}
	var having []string
	if f.HasErrors {
		having = append(having, "error_total > 0")
	}
	if f.DistinctID != "" {
		having = append(having, "has(users, ?)")
		args = append(args, f.DistinctID)
	}
	if f.URL != "" {
		having = append(having, "arrayExists(u -> positionCaseInsensitive(u, ?) > 0, url_list)")
		args = append(args, f.URL)
	}
	if f.MinDurationMs > 0 {
		having = append(having, "duration_ms >= ?")
		args = append(args, f.MinDurationMs)
	}
	q := `SELECT session_id,
		argMax(distinct_id, end) AS user_id,
		groupUniqArray(distinct_id) AS users,
		min(start) AS first_at, max(end) AS last_at,
		toInt64(dateDiff('millisecond', min(start), max(end))) AS duration_ms,
		toUInt64(sum(pages)) AS page_total, toUInt64(sum(errors)) AS error_total,
		count() AS chunks, toUInt64(sum(bytes)) AS byte_total,
		arraySlice(groupUniqArrayArray(urls), 1, 20) AS url_list
	FROM replay_chunks WHERE ` + strings.Join(where, " AND ") + `
	GROUP BY session_id`
	if len(having) > 0 {
		q += " HAVING " + strings.Join(having, " AND ")
	}
	q += " ORDER BY first_at DESC LIMIT ?"
	args = append(args, f.Limit)
	return q, args
}

func (c *Client) ListReplays(ctx context.Context, f ReplayFilter) ([]ReplaySummary, error) {
	q, args := ReplaysSQL(f)
	var rows []struct {
		ReplaySummary
		Users []string `ch:"users"`
	}
	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	out := make([]ReplaySummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ReplaySummary)
	}
	return out, nil
}

// ReplayChunk is a stored chunk in playback order.
type ReplayChunk struct {
	Key   string    `ch:"chunk_key"`
	Start time.Time `ch:"start"`
	End   time.Time `ch:"end"`
}

func (c *Client) ReplayChunks(ctx context.Context, project uint64, session string) ([]ReplayChunk, error) {
	var out []ReplayChunk
	err := c.conn.Select(ctx, &out, `SELECT chunk_key, start, end FROM replay_chunks
		WHERE project_id = ? AND session_id = ? ORDER BY start, chunk_key LIMIT 2000`, project, session)
	return out, err
}

func (c *Client) ErrorsBySession(ctx context.Context, project uint64, session string) ([]ErrorRecord, error) {
	var out []ErrorRecord
	err := c.conn.Select(ctx, &out, `SELECT `+errorCols+` FROM errors
		WHERE project_id = ? AND session_id = ? ORDER BY timestamp LIMIT 500`, project, session)
	return out, err
}

// SessionTrace is one trace started during a session.
type SessionTrace struct {
	TraceID    string    `ch:"trace_id"`
	Start      time.Time `ch:"first_at"`
	Name       string    `ch:"name"`
	Service    string    `ch:"service"`
	DurationNs uint64    `ch:"total_ns"`
	Spans      uint64    `ch:"spans"`
	ErrorSpans uint64    `ch:"error_spans"`
}

// TracesBySession lists the traces whose spans carry the session id, with the
// earliest span as the trace's name.
func (c *Client) TracesBySession(ctx context.Context, project uint64, session string) ([]SessionTrace, error) {
	var out []SessionTrace
	err := c.conn.Select(ctx, &out, `SELECT trace_id, min(timestamp) AS first_at,
			argMin(span_name, timestamp) AS name, argMin(service_name, timestamp) AS service,
			toUInt64(max(toUnixTimestamp64Nano(timestamp) + duration_ns) - min(toUnixTimestamp64Nano(timestamp))) AS total_ns,
			count() AS spans, countIf(status_code = 'ERROR') AS error_spans
		FROM spans WHERE project_id = ? AND trace_id IN (
			SELECT DISTINCT trace_id FROM spans WHERE project_id = ? AND session_id = ?)
		GROUP BY trace_id ORDER BY first_at LIMIT 500`, project, project, session)
	return out, err
}
