package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// ReplayReader reads replay metadata from ClickHouse.
type ReplayReader interface {
	ListReplays(ctx context.Context, f ch.ReplayFilter) ([]ch.ReplaySummary, error)
	ReplayChunks(ctx context.Context, project uint64, session string) ([]ch.ReplayChunk, error)
	ErrorsBySession(ctx context.Context, project uint64, session string) ([]ch.ErrorRecord, error)
	TracesBySession(ctx context.Context, project uint64, session string) ([]ch.SessionTrace, error)
}

// ObjectGetter reads stored chunks.
type ObjectGetter interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

// DefaultReplayRetentionDays matches the TTL of the replay_chunks table.
const DefaultReplayRetentionDays = 30

var sessionIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (s *Server) retentionDays() int {
	if s.ReplayRetentionDays > 0 {
		return s.ReplayRetentionDays
	}
	return DefaultReplayRetentionDays
}

func replaySummary(r ch.ReplaySummary) gen.ReplaySummary {
	urls := r.URLs
	if urls == nil {
		urls = []string{}
	}
	return gen.ReplaySummary{SessionId: r.SessionID, DistinctId: r.DistinctID, Start: r.Start, End: r.End,
		DurationMs: r.DurationMs, PageCount: int64(r.Pages), ErrorCount: int64(r.Errors), ChunkCount: int64(r.Chunks), Urls: urls}
}

func (s *Server) ListReplays(w http.ResponseWriter, r *http.Request, params gen.ListReplaysParams) {
	p, ok := project(w, r, params.ProjectId)
	if !ok {
		return
	}
	if s.Replays == nil {
		writeJSON(w, http.StatusOK, gen.ReplayList{Replays: []gen.ReplaySummary{}, RetentionDays: s.retentionDays()})
		return
	}
	from, to := s.timeRange(params.From, params.To)
	f := ch.ReplayFilter{Project: p, From: from, To: to, Limit: limit(params.Limit, 50, 200)}
	if params.HasErrors != nil {
		f.HasErrors = *params.HasErrors
	}
	if params.User != nil {
		f.DistinctID = *params.User
	}
	if params.Url != nil {
		f.URL = *params.Url
	}
	if params.MinDurationMs != nil {
		f.MinDurationMs = *params.MinDurationMs
	}
	rows, err := s.Replays.ListReplays(r.Context(), f)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.ReplayList{Replays: make([]gen.ReplaySummary, 0, len(rows)), RetentionDays: s.retentionDays()}
	for _, row := range rows {
		out.Replays = append(out.Replays, replaySummary(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// findReplay resolves the session within the user's project and writes 404
// when it has no recording.
func (s *Server) findReplay(w http.ResponseWriter, r *http.Request, session string, pid *gen.ProjectID) (uint64, *ch.ReplaySummary, bool) {
	p, ok := project(w, r, pid)
	if !ok {
		return 0, nil, false
	}
	if s.Replays == nil || !sessionIDRe.MatchString(session) {
		writeErr(w, http.StatusNotFound, "replay not found")
		return 0, nil, false
	}
	// Whole retention window: a link from an old issue still works.
	to := s.now().UTC().AddDate(0, 0, 1)
	rows, err := s.Replays.ListReplays(r.Context(), ch.ReplayFilter{Project: p, SessionID: session,
		From: to.AddDate(0, 0, -s.retentionDays()-2), To: to, Limit: 1})
	if err != nil {
		internal(w, err)
		return 0, nil, false
	}
	if len(rows) == 0 {
		writeErr(w, http.StatusNotFound, "replay not found")
		return 0, nil, false
	}
	return p, &rows[0], true
}

func (s *Server) GetReplay(w http.ResponseWriter, r *http.Request, session string, params gen.GetReplayParams) {
	p, sum, ok := s.findReplay(w, r, session, params.ProjectId)
	if !ok {
		return
	}
	errs, err := s.Replays.ErrorsBySession(r.Context(), p, session)
	if err != nil {
		internal(w, err)
		return
	}
	traces, err := s.Replays.TracesBySession(r.Context(), p, session)
	if err != nil {
		internal(w, err)
		return
	}
	out := gen.ReplayDetail{Summary: replaySummary(*sum), Errors: make([]gen.ErrorOccurrence, 0, len(errs)),
		Traces: make([]gen.SessionTrace, 0, len(traces)), RetentionDays: s.retentionDays()}
	for _, e := range errs {
		out.Errors = append(out.Errors, errorOccurrence(e))
	}
	for _, t := range traces {
		out.Traces = append(out.Traces, gen.SessionTrace{TraceId: t.TraceID, Start: t.Start, Name: t.Name, ServiceName: t.Service,
			DurationNs: int64(t.DurationNs), SpanCount: int64(t.Spans), ErrorCount: int64(t.ErrorSpans)})
	}
	writeJSON(w, http.StatusOK, out)
}

// GetReplayEvents streams every chunk's events as one JSON array. A chunk
// that cannot be read is skipped, leaving a gap rather than failing playback.
func (s *Server) GetReplayEvents(w http.ResponseWriter, r *http.Request, session string, params gen.GetReplayEventsParams) {
	p, _, ok := s.findReplay(w, r, session, params.ProjectId)
	if !ok {
		return
	}
	if s.ReplayObjects == nil {
		writeErr(w, http.StatusServiceUnavailable, "replay storage is not configured")
		return
	}
	chunks, err := s.Replays.ReplayChunks(r.Context(), p, session)
	if err != nil {
		internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write([]byte("["))
	first := true
	for _, c := range chunks {
		events, err := s.readChunk(r.Context(), c.Key)
		if err != nil {
			slog.Error("replay chunk", "key", c.Key, "err", err)
			continue
		}
		for _, e := range events {
			if !first {
				_, _ = w.Write([]byte(","))
			}
			first = false
			_, _ = w.Write(e)
		}
	}
	_, _ = w.Write([]byte("]"))
}

func (s *Server) readChunk(ctx context.Context, key string) ([]json.RawMessage, error) {
	data, err := s.ReplayObjects.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(zr, replay.MaxDecompressed+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > replay.MaxDecompressed {
		return nil, errors.New("chunk too large")
	}
	var events []json.RawMessage
	return events, json.Unmarshal(raw, &events)
}
