package ch

import (
	"context"
	"fmt"
	"time"
)

// LinkedIDs returns every distinct id linked to any of ids through
// identify or alias, including the ids and their person keys.
func (c *Client) LinkedIDs(ctx context.Context, project uint64, ids []string) ([]string, error) {
	seen := map[string]bool{}
	frontier := []string{}
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			frontier = append(frontier, id)
		}
	}
	// Links are one level deep (id -> person key), so two rounds reach every
	// id of the person; a few more cover keys that were later re-linked.
	for round := 0; round < 4 && len(frontier) > 0; round++ {
		var rows []struct {
			ID  string `ch:"distinct_id"`
			Key string `ch:"person_key"`
		}
		err := c.conn.Select(ctx, &rows, `SELECT distinct_id, person_key FROM person_aliases FINAL
			WHERE project_id = ? AND (distinct_id IN (?) OR person_key IN (?))`, project, frontier, frontier)
		if err != nil {
			return nil, err
		}
		frontier = frontier[:0]
		for _, r := range rows {
			for _, v := range []string{r.ID, r.Key} {
				if v != "" && !seen[v] {
					seen[v] = true
					frontier = append(frontier, v)
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out, nil
}

// PersonData is what a deletion found for a person.
type PersonData struct {
	Sessions  []string
	Traces    []string
	ChunkKeys []string
}

func (c *Client) strings(ctx context.Context, q string, args ...interface{}) ([]string, error) {
	var rows []struct {
		V string `ch:"v"`
	}
	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.V != "" {
			out = append(out, r.V)
		}
	}
	return out, nil
}

// FindPersonData collects the sessions, traces and replay objects of ids.
func (c *Client) FindPersonData(ctx context.Context, project uint64, ids []string) (PersonData, error) {
	var d PersonData
	var err error
	d.Sessions, err = c.strings(ctx, `SELECT DISTINCT session_id AS v FROM (
			SELECT session_id FROM events WHERE project_id = ? AND distinct_id IN (?)
			UNION ALL SELECT session_id FROM replay_chunks WHERE project_id = ? AND distinct_id IN (?)
			UNION ALL SELECT session_id FROM sessions WHERE project_id = ? AND distinct_id IN (?)
			UNION ALL SELECT session_id FROM errors WHERE project_id = ? AND distinct_id IN (?))`,
		project, ids, project, ids, project, ids, project, ids)
	if err != nil {
		return d, err
	}
	sessions := append([]string{""}, d.Sessions...) // IN () needs a value
	d.Traces, err = c.strings(ctx, `SELECT DISTINCT trace_id AS v FROM (
			SELECT trace_id FROM spans WHERE project_id = ? AND (session_id IN (?) OR attributes['enduser.id'] IN (?))
			UNION ALL SELECT trace_id FROM events WHERE project_id = ? AND distinct_id IN (?))`,
		project, sessions, ids, project, ids)
	if err != nil {
		return d, err
	}
	d.ChunkKeys, err = c.strings(ctx, `SELECT DISTINCT chunk_key AS v FROM replay_chunks
		WHERE project_id = ? AND (session_id IN (?) OR distinct_id IN (?))`, project, sessions, ids)
	return d, err
}

// personTables lists what a deletion removes, with the condition that ties
// rows to the person. Arguments are filled by DeletePersonData.
var personTables = []struct{ table, cond string }{
	{"events", "distinct_id IN (?) OR session_id IN (?)"},
	{"sessions", "distinct_id IN (?) OR session_id IN (?)"},
	{"replay_chunks", "distinct_id IN (?) OR session_id IN (?)"},
	{"errors", "distinct_id IN (?) OR session_id IN (?) OR trace_id IN (?)"},
	{"spans", "session_id IN (?) OR trace_id IN (?) OR attributes['enduser.id'] IN (?)"},
	{"logs", "trace_id IN (?) OR JSONExtractString(attributes, 'enduser.id') IN (?)"},
	{"person_aliases", "distinct_id IN (?) OR person_key IN (?)"},
}

// DeletePersonData deletes the rows of a person in every table and returns
// how many rows each table had.
func (c *Client) DeletePersonData(ctx context.Context, project uint64, ids []string, d PersonData) (map[string]int64, error) {
	sessions := append([]string{""}, d.Sessions...)
	traces := append([]string{""}, d.Traces...)
	ids = append([]string{}, ids...)
	argsFor := map[string][]interface{}{
		"events":         {ids, sessions},
		"sessions":       {ids, sessions},
		"replay_chunks":  {ids, sessions},
		"errors":         {ids, sessions, traces},
		"spans":          {sessions, traces, ids},
		"logs":           {traces, ids},
		"person_aliases": {ids, ids},
	}
	out := map[string]int64{}
	for _, t := range personTables {
		args := append([]interface{}{project}, argsFor[t.table]...)
		where := fmt.Sprintf("project_id = ? AND (%s)", t.cond)
		var n []struct {
			N uint64 `ch:"n"`
		}
		if err := c.conn.Select(ctx, &n, "SELECT count() AS n FROM "+t.table+" WHERE "+where, args...); err != nil {
			return out, fmt.Errorf("%s: %w", t.table, err)
		}
		out[t.table] = int64(n[0].N)
		if n[0].N == 0 {
			continue
		}
		if err := c.conn.Exec(ctx, "DELETE FROM "+t.table+" WHERE "+where, args...); err != nil {
			return out, fmt.Errorf("%s: %w", t.table, err)
		}
	}
	return out, nil
}

// RetentionTables maps a kind of data to its tables and time columns.
var RetentionTables = map[string][][2]string{
	"events":  {{"events", "timestamp"}, {"sessions", "started_at"}},
	"traces":  {{"spans", "timestamp"}},
	"logs":    {{"logs", "timestamp"}},
	"errors":  {{"errors", "timestamp"}},
	"replays": {{"replay_chunks", "start"}},
}

// DeleteOlder removes a project's rows of one kind older than cutoff and
// returns how many there were. It only deletes when there is something to
// delete, since each delete is a mutation.
func (c *Client) DeleteOlder(ctx context.Context, project uint64, kind string, cutoff time.Time) (int64, error) {
	tables, ok := RetentionTables[kind]
	if !ok {
		return 0, fmt.Errorf("unknown data kind %q", kind)
	}
	var total int64
	for _, t := range tables {
		where := "project_id = ? AND " + t[1] + " < ?"
		var n []struct {
			N uint64 `ch:"n"`
		}
		if err := c.conn.Select(ctx, &n, "SELECT count() AS n FROM "+t[0]+" WHERE "+where, project, cutoff); err != nil {
			return total, err
		}
		if n[0].N == 0 {
			continue
		}
		if err := c.conn.Exec(ctx, "DELETE FROM "+t[0]+" WHERE "+where, project, cutoff); err != nil {
			return total, err
		}
		total += int64(n[0].N)
	}
	return total, nil
}
