// Package replay parses session replay chunks sent by the browser SDK,
// summarises them for ClickHouse and stores the raw events in S3-compatible
// object storage.
package replay

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"time"
)

const (
	// MaxCompressed is the largest request body accepted.
	MaxCompressed = 2 << 20
	// MaxDecompressed bounds gzip expansion.
	MaxDecompressed = 16 << 20
	// MaxEvents per chunk.
	MaxEvents = 50_000
	// MarkerTag is the rrweb custom event tag the SDK uses for timeline markers.
	MarkerTag = "pulse"
)

// rrweb event types used here.
const (
	typeFullSnapshot = 2
	typeMeta         = 4
	typeCustom       = 5
)

var sessionRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrInvalid is returned for chunks that cannot be accepted.
type ErrInvalid struct{ Msg string }

func (e *ErrInvalid) Error() string { return e.Msg }

// Event is one rrweb event; Data is kept raw.
type Event struct {
	Type      int             `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// Chunk is what the SDK posts.
type Chunk struct {
	SessionID  string            `json:"session_id"`
	DistinctID string            `json:"distinct_id"`
	Start      int64             `json:"start"`
	End        int64             `json:"end"`
	Events     []json.RawMessage `json:"events"`
}

// Meta summarises a chunk for the replay_chunks table.
type Meta struct {
	ProjectID  uint64
	SessionID  string
	DistinctID string
	Key        string
	Start      time.Time
	End        time.Time
	Events     uint32
	Pages      uint16
	Errors     uint16
	URLs       []string
	TraceIDs   []string
	Bytes      uint32
}

// Decode reads a request body, gunzipping it when gzipped is set.
func Decode(body []byte, gzipped bool) (Chunk, error) {
	var c Chunk
	raw := body
	if gzipped {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return c, &ErrInvalid{"body is not gzip"}
		}
		raw, err = io.ReadAll(io.LimitReader(zr, MaxDecompressed+1))
		if err != nil {
			return c, &ErrInvalid{"body is not valid gzip"}
		}
		if len(raw) > MaxDecompressed {
			return c, &ErrInvalid{"chunk too large"}
		}
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, &ErrInvalid{"chunk is not valid JSON"}
	}
	if !sessionRe.MatchString(c.SessionID) {
		return c, &ErrInvalid{"session_id is missing or malformed"}
	}
	if len(c.Events) == 0 || len(c.Events) > MaxEvents {
		return c, &ErrInvalid{fmt.Sprintf("chunk must have 1 to %d events", MaxEvents)}
	}
	if len(c.DistinctID) > 200 {
		c.DistinctID = c.DistinctID[:200]
	}
	return c, nil
}

type marker struct {
	Tag     string `json:"tag"`
	Payload struct {
		Kind    string `json:"kind"`
		TraceID string `json:"trace_id"`
	} `json:"payload"`
}

// Summarise computes the chunk metadata. Times come from the events, so a
// client clock that disagrees with the chunk header does not matter.
func Summarise(project uint64, c Chunk) Meta {
	m := Meta{ProjectID: project, SessionID: c.SessionID, DistinctID: c.DistinctID, Events: uint32(len(c.Events))}
	var lo, hi int64
	urls := map[string]bool{}
	traces := map[string]bool{}
	for _, raw := range c.Events {
		var e Event
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if e.Timestamp > 0 && (lo == 0 || e.Timestamp < lo) {
			lo = e.Timestamp
		}
		if e.Timestamp > hi {
			hi = e.Timestamp
		}
		switch e.Type {
		case typeMeta:
			var d struct {
				Href string `json:"href"`
			}
			if json.Unmarshal(e.Data, &d) == nil && d.Href != "" {
				m.Pages++
				if u := cleanURL(d.Href); u != "" && len(urls) < 50 {
					urls[u] = true
				}
			}
		case typeCustom:
			var mk marker
			if json.Unmarshal(e.Data, &mk) == nil && mk.Tag == MarkerTag {
				if mk.Payload.Kind == "error" {
					m.Errors++
				}
				if t := mk.Payload.TraceID; len(t) == 32 && len(traces) < 200 {
					traces[t] = true
				}
			}
		}
	}
	if lo == 0 {
		lo, hi = c.Start, c.End
	}
	m.Start, m.End = time.UnixMilli(lo).UTC(), time.UnixMilli(hi).UTC()
	m.URLs, m.TraceIDs = keys(urls), keys(traces)
	return m
}

// HasFullSnapshot reports whether a chunk can start playback on its own.
func HasFullSnapshot(c Chunk) bool {
	for _, raw := range c.Events {
		var e Event
		if json.Unmarshal(raw, &e) == nil && e.Type == typeFullSnapshot {
			return true
		}
	}
	return false
}

// cleanURL drops the query string and fragment, which can carry tokens.
func cleanURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host + u.Path
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ObjectKey is where a chunk is stored. Keys sort by start time within a
// session, so listing them returns playback order.
func ObjectKey(project uint64, session string, start time.Time, nonce string) string {
	return fmt.Sprintf("replays/%d/%s/%013d-%s.json.gz", project, session, start.UnixMilli(), nonce)
}

// Encode gzips the events of a chunk for storage.
func Encode(c Chunk) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(c.Events); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ErrNotFound is returned by stores for missing objects.
var ErrNotFound = errors.New("replay not found")

// ObjectPutter writes objects; *S3 implements it.
type ObjectPutter interface {
	Put(ctx context.Context, key string, data []byte) error
}

// MetaInserter records chunk metadata; the ClickHouse client implements it.
type MetaInserter interface {
	InsertReplayChunk(ctx context.Context, m Meta) error
}

// Writer stores a chunk's events and then its metadata, so metadata never
// points at a missing object.
type Writer struct {
	Objects ObjectPutter
	Meta    MetaInserter
}

func (w *Writer) Store(ctx context.Context, project uint64, c Chunk) error {
	m := Summarise(project, c)
	data, err := Encode(c)
	if err != nil {
		return err
	}
	nonce := make([]byte, 4)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	m.Key = ObjectKey(project, c.SessionID, m.Start, hex.EncodeToString(nonce))
	m.Bytes = uint32(len(data))
	if err := w.Objects.Put(ctx, m.Key, data); err != nil {
		return fmt.Errorf("store replay chunk: %w", err)
	}
	return w.Meta.InsertReplayChunk(ctx, m)
}
