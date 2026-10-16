// Package service implements the replay use cases.
package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/replay/domain"
	"github.com/Mutiur03/pulse/internal/modules/replay/ports"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Service lists recordings and reads their events. A nil Reader disables
// replay: listings are empty and sessions are not found.
type Service struct {
	reader    ports.Reader
	objects   ports.Objects
	retention int
	now       func() time.Time
}

// New returns a Service; retention is in days (0 uses the default).
func New(reader ports.Reader, objects ports.Objects, retention int, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{reader: reader, objects: objects, retention: retention, now: now}
}

// RetentionDays is how long recordings are kept.
func (s *Service) RetentionDays() int { return domain.RetentionDays(s.retention) }

// Enabled reports whether replay metadata can be read.
func (s *Service) Enabled() bool { return s.reader != nil }

// List returns the recordings matching f; nothing when replay is off.
func (s *Service) List(ctx context.Context, f ch.ReplayFilter) ([]ch.ReplaySummary, error) {
	if s.reader == nil {
		return nil, nil
	}
	return s.reader.ListReplays(ctx, f)
}

// Find resolves a session within the project over the whole retention
// window, so a link from an old issue still works.
func (s *Service) Find(ctx context.Context, project uint64, session string) (ch.ReplaySummary, error) {
	if s.reader == nil || !domain.ValidSessionID(session) {
		return ch.ReplaySummary{}, domain.ErrNotFound
	}
	to := s.now().UTC().AddDate(0, 0, 1)
	rows, err := s.reader.ListReplays(ctx, ch.ReplayFilter{Project: project, SessionID: session,
		From: to.AddDate(0, 0, -s.RetentionDays()-2), To: to, Limit: 1})
	if err != nil {
		return ch.ReplaySummary{}, err
	}
	if len(rows) == 0 {
		return ch.ReplaySummary{}, domain.ErrNotFound
	}
	return rows[0], nil
}

// Detail is a session's errors and traces.
type Detail struct {
	Errors []ch.ErrorRecord
	Traces []ch.SessionTrace
}

// Detail loads the errors and traces of a found session.
func (s *Service) Detail(ctx context.Context, project uint64, session string) (Detail, error) {
	errs, err := s.reader.ErrorsBySession(ctx, project, session)
	if err != nil {
		return Detail{}, err
	}
	traces, err := s.reader.TracesBySession(ctx, project, session)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Errors: errs, Traces: traces}, nil
}

// Chunks lists the stored chunk keys of a found session in playback order.
func (s *Service) Chunks(ctx context.Context, project uint64, session string) ([]string, error) {
	if s.objects == nil {
		return nil, domain.ErrStorageOff
	}
	chunks, err := s.reader.ReplayChunks(ctx, project, session)
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(chunks))
	for i, c := range chunks {
		keys[i] = c.Key
	}
	return keys, nil
}

// Events reads one chunk's events. Callers skip a chunk that fails,
// leaving a gap rather than failing playback.
func (s *Service) Events(ctx context.Context, key string) ([]json.RawMessage, error) {
	data, err := s.objects.Get(ctx, key)
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

// EachEvent calls fn for every event of every chunk, logging and skipping
// chunks that cannot be read.
func (s *Service) EachEvent(ctx context.Context, keys []string, fn func(json.RawMessage)) {
	for _, k := range keys {
		events, err := s.Events(ctx, k)
		if err != nil {
			slog.Error("replay chunk", "key", k, "err", err)
			continue
		}
		for _, e := range events {
			fn(e)
		}
	}
}
