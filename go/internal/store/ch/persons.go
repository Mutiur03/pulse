package ch

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Columns are event properties also stored in their own columns so they
// can be filtered and grouped cheaply.
type Columns struct {
	CurrentURL, Release, Country, Browser, OS string
}

// EventColumns picks the column values from an event's properties.
func EventColumns(props map[string]interface{}) Columns {
	get := func(k string, max int) string {
		v, _ := props[k].(string)
		if len(v) > max {
			v = v[:max]
		}
		return v
	}
	return Columns{
		CurrentURL: get("$current_url", 2048), Release: get("$release", 200), Country: get("$country", 2),
		Browser: get("$browser", 64), OS: get("$os", 64),
	}
}

// Alias maps a distinct id to a person key.
type Alias struct {
	ProjectID  uint64
	DistinctID string
	PersonKey  string
	At         time.Time
}

// AliasesFromEvents finds identity links in raw events: $identify with
// $anon_distinct_id links the anonymous id to the identified one, and
// $create_alias links properties.alias to the event's distinct_id.
func AliasesFromEvents(events []map[string]interface{}) []Alias {
	var out []Alias
	for _, e := range events {
		name, _ := e["event"].(string)
		did, _ := e["distinct_id"].(string)
		props, _ := e["properties"].(map[string]interface{})
		var other string
		switch name {
		case "$identify":
			other, _ = props["$anon_distinct_id"].(string)
		case "$create_alias":
			other, _ = props["alias"].(string)
		default:
			continue
		}
		other, did = strings.TrimSpace(other), strings.TrimSpace(did)
		if other == "" || did == "" || other == did || len(other) > 200 || len(did) > 200 {
			continue
		}
		var pid uint64
		if s, ok := e["project_id"].(string); ok {
			_, _ = fmt.Sscan(s, &pid)
		}
		at := time.Now().UTC()
		if ts, ok := e["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				at = t
			}
		}
		out = append(out, Alias{ProjectID: pid, DistinctID: other, PersonKey: did, At: at})
	}
	return out
}

// InsertAliases stores identity links. The target is first resolved to its
// own person, so a chain anon -> user -> account collapses to one level.
func (c *Client) InsertAliases(ctx context.Context, aliases []Alias) error {
	if len(aliases) == 0 {
		return nil
	}
	for i, a := range aliases {
		var rows []struct {
			Key string `ch:"person_key"`
		}
		if err := c.conn.Select(ctx, &rows, `SELECT argMax(person_key, updated_at) AS person_key FROM person_aliases
			WHERE project_id = ? AND distinct_id = ? GROUP BY distinct_id`, a.ProjectID, a.PersonKey); err != nil {
			return err
		}
		if len(rows) == 1 && rows[0].Key != "" && rows[0].Key != a.DistinctID {
			aliases[i].PersonKey = rows[0].Key
		}
	}
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO person_aliases (project_id, distinct_id, person_key, updated_at)")
	if err != nil {
		return err
	}
	for _, a := range aliases {
		if err := batch.Append(a.ProjectID, a.DistinctID, a.PersonKey, a.At); err != nil {
			return err
		}
	}
	return batch.Send()
}

// personEvents is a subquery of a project's events in a time range with
// each event's resolved person key as "person". Its arguments are project,
// project, from, to.
const personEvents = `(SELECT e.*, if(a.person_key != '', a.person_key, e.distinct_id) AS person
	FROM events AS e
	LEFT JOIN (SELECT distinct_id, argMax(person_key, updated_at) AS person_key FROM person_aliases
		WHERE project_id = ? GROUP BY distinct_id) AS a ON a.distinct_id = e.distinct_id
	WHERE e.project_id = ? AND e.timestamp >= ? AND e.timestamp < ? AND e.distinct_id != '')`

// PersonsFrom is the events table of one project with a person column: the
// person key each distinct id was linked to, or the id itself. Its one
// argument is the project id.
const PersonsFrom = `(SELECT e.*, if(a.person_key != '', a.person_key, e.distinct_id) AS person
	FROM events AS e
	LEFT JOIN (SELECT distinct_id, argMax(person_key, updated_at) AS person_key FROM person_aliases
		WHERE project_id = ? GROUP BY distinct_id) AS a ON a.distinct_id = e.distinct_id)`

func personArgs(project uint64, from, to time.Time) []interface{} {
	return []interface{}{project, project, from, to}
}

// PersonRow summarizes one person.
type PersonRow struct {
	Person      string    `ch:"person"`
	DistinctIDs []string  `ch:"distinct_ids"`
	FirstSeen   time.Time `ch:"first_seen"`
	LastSeen    time.Time `ch:"last_seen"`
	Events      uint64    `ch:"events"`
	Sessions    uint64    `ch:"sessions"`
	Traits      string    `ch:"traits"`
	Browser     string    `ch:"browser"`
	OS          string    `ch:"os"`
	Country     string    `ch:"country"`
	LastURL     string    `ch:"last_url"`
}

const personCols = `person,
	arraySort(groupUniqArray(distinct_id)) AS distinct_ids,
	min(timestamp) AS first_seen, max(timestamp) AS last_seen, count() AS events,
	uniqIf(session_id, session_id != '') AS sessions,
	argMaxIf(properties, timestamp, event = '$identify') AS traits,
	argMaxIf(JSONExtractString(properties, '$browser'), timestamp, JSONExtractString(properties, '$browser') != '') AS browser,
	argMaxIf(JSONExtractString(properties, '$os'), timestamp, JSONExtractString(properties, '$os') != '') AS os,
	argMaxIf(JSONExtractString(properties, '$country'), timestamp, JSONExtractString(properties, '$country') != '') AS country,
	argMaxIf(JSONExtractString(properties, '$current_url'), timestamp, JSONExtractString(properties, '$current_url') != '') AS last_url`

// PersonFilter selects persons for the persons list.
type PersonFilter struct {
	Project  uint64
	From, To time.Time
	Search   string
	Limit    int
}

// PersonsSQL lists persons seen in the range, most recently active first.
// Search matches the person key, any of its distinct ids or its traits.
func PersonsSQL(f PersonFilter) (string, []interface{}) {
	args := personArgs(f.Project, f.From, f.To)
	q := "SELECT " + personCols + " FROM " + personEvents + " GROUP BY person"
	if f.Search != "" {
		q += ` HAVING positionCaseInsensitive(person, ?) > 0 OR positionCaseInsensitive(traits, ?) > 0
			OR arrayExists(d -> positionCaseInsensitive(d, ?) > 0, distinct_ids)`
		args = append(args, f.Search, f.Search, f.Search)
	}
	q += " ORDER BY last_seen DESC LIMIT ?"
	return q, append(args, f.Limit)
}

// ListPersons runs PersonsSQL.
func (c *Client) ListPersons(ctx context.Context, f PersonFilter) ([]PersonRow, error) {
	q, args := PersonsSQL(f)
	var out []PersonRow
	err := c.conn.Select(ctx, &out, q, args...)
	return out, err
}

// GetPerson summarizes one person; ok is false when it has no events in
// the range.
func (c *Client) GetPerson(ctx context.Context, project uint64, person string, from, to time.Time) (PersonRow, bool, error) {
	var out []PersonRow
	args := append(personArgs(project, from, to), person)
	err := c.conn.Select(ctx, &out, "SELECT "+personCols+" FROM "+personEvents+" WHERE person = ? GROUP BY person", args...)
	if err != nil || len(out) == 0 {
		return PersonRow{}, false, err
	}
	return out[0], true, nil
}

// PersonSession is one session of a person.
type PersonSession struct {
	SessionID string    `ch:"session_id"`
	Start     time.Time `ch:"session_start"`
	End       time.Time `ch:"session_end"`
	Events    uint64    `ch:"events"`
	Pageviews uint64    `ch:"pageviews"`
	EntryPath string    `ch:"entry_path"`
	ExitPath  string    `ch:"exit_path"`
	Replay    uint8     `ch:"has_replay"`
	Errors    uint64    `ch:"errors"`
}

// PersonSessions lists a person's most recent sessions with replay and
// error counts.
func (c *Client) PersonSessions(ctx context.Context, project uint64, person string, from, to time.Time, limit int) ([]PersonSession, error) {
	args := append(personArgs(project, from, to), person, limit, project, project, from)
	var out []PersonSession
	err := c.conn.Select(ctx, &out, `SELECT s.session_id AS session_id, s.session_start AS session_start, s.session_end AS session_end, s.events AS events,
		s.pageviews AS pageviews, s.entry_path AS entry_path, s.exit_path AS exit_path, r.has_replay AS has_replay, e.errors AS errors FROM (
		SELECT session_id, min(timestamp) AS session_start, max(timestamp) AS session_end, count() AS events,
			countIf(event = '$pageview') AS pageviews,
			argMinIf(JSONExtractString(properties, '$pathname'), timestamp, event = '$pageview') AS entry_path,
			argMaxIf(JSONExtractString(properties, '$pathname'), timestamp, event = '$pageview') AS exit_path
		FROM `+personEvents+` WHERE person = ? AND session_id != ''
		GROUP BY session_id ORDER BY session_start DESC LIMIT ?) AS s
	LEFT JOIN (SELECT session_id, toUInt8(1) AS has_replay FROM replay_chunks WHERE project_id = ? GROUP BY session_id) AS r ON r.session_id = s.session_id
	LEFT JOIN (SELECT session_id, count() AS errors FROM errors WHERE project_id = ? AND timestamp >= ? AND session_id != '' GROUP BY session_id) AS e ON e.session_id = s.session_id
	ORDER BY s.session_start DESC`, args...)
	return out, err
}

// PersonEvents lists a person's most recent events across all its ids.
func (c *Client) PersonEvents(ctx context.Context, project uint64, person string, from, to time.Time, limit int) ([]EventRecord, error) {
	args := append(personArgs(project, from, to), person, limit)
	var out []EventRecord
	err := c.conn.Select(ctx, &out, `SELECT event, timestamp, distinct_id, session_id, span_id, properties
		FROM `+personEvents+` WHERE person = ? ORDER BY timestamp DESC LIMIT ?`, args...)
	return out, err
}

// ResolvePerson returns the person key a distinct id belongs to (itself
// when it was never linked).
func (c *Client) ResolvePerson(ctx context.Context, project uint64, distinctID string) (string, error) {
	var rows []struct {
		Key string `ch:"person_key"`
	}
	err := c.conn.Select(ctx, &rows, `SELECT argMax(person_key, updated_at) AS person_key FROM person_aliases
		WHERE project_id = ? AND distinct_id = ? GROUP BY distinct_id`, project, distinctID)
	if err != nil || len(rows) == 0 || rows[0].Key == "" {
		return distinctID, err
	}
	return rows[0].Key, nil
}
