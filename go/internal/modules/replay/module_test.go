package replay_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/modules/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/go-chi/chi/v5"
)

type fakeReplays struct{ last ch.ReplayFilter }

func (f *fakeReplays) ListReplays(_ context.Context, rf ch.ReplayFilter) ([]ch.ReplaySummary, error) {
	f.last = rf
	if rf.Project != 1 || (rf.SessionID != "" && rf.SessionID != "s-1") {
		return nil, nil
	}
	return []ch.ReplaySummary{{SessionID: "s-1", DistinctID: "u", Errors: 1, Chunks: 3, DurationMs: 9000}}, nil
}

func (f *fakeReplays) ReplayChunks(context.Context, uint64, string) ([]ch.ReplayChunk, error) {
	return []ch.ReplayChunk{{Key: "a"}, {Key: "missing"}, {Key: "b"}}, nil
}

func (f *fakeReplays) ErrorsBySession(context.Context, uint64, string) ([]ch.ErrorRecord, error) {
	return []ch.ErrorRecord{{Message: "boom", TraceID: "t1", Timestamp: time.Unix(5, 0)}}, nil
}

func (f *fakeReplays) TracesBySession(context.Context, uint64, string) ([]ch.SessionTrace, error) {
	return []ch.SessionTrace{{TraceID: "t1", Name: "click", Spans: 4, ErrorSpans: 1}}, nil
}

type memChunks map[string]string

func (m memChunks) Get(_ context.Context, k string) ([]byte, error) {
	v, ok := m[k]
	if !ok {
		return nil, errors.New("no such key")
	}
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(v))
	_ = w.Close()
	return b.Bytes(), nil
}

func TestReplays(t *testing.T) {
	fr := &fakeReplays{}
	m := replay.New(replay.Deps{Reader: fr, Objects: memChunks{"a": `[{"type":4,"timestamp":1},{"type":2,"timestamp":2}]`, "b": `[{"type":3,"timestamp":3}]`}})
	me := &auth.Principal{Projects: []auth.Project{{ID: 1}}}

	rec := call(t, m, me, "GET", "/v1/replays?has_errors=true&user=u&url=cart&min_duration_ms=5000", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"session_id":"s-1"`) || !strings.Contains(rec.Body.String(), `"retention_days":30`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if !fr.last.HasErrors || fr.last.DistinctID != "u" || fr.last.URL != "cart" || fr.last.MinDurationMs != 5000 {
		t.Errorf("filters: %+v", fr.last)
	}
	rec = call(t, m, me, "GET", "/v1/replays/s-1", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"message":"boom"`) || !strings.Contains(rec.Body.String(), `"span_count":4`) {
		t.Errorf("detail: %d %s", rec.Code, rec.Body)
	}
	rec = call(t, m, me, "GET", "/v1/replays/s-1/events", "")
	var events []map[string]any
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &events) != nil || len(events) != 3 {
		t.Errorf("events (a missing chunk is skipped): %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/v1/replays/other", "/v1/replays/s-1?project_id=2", "/v1/replays/bad%20id/events", "/v1/replays?project_id=2"} {
		if rec := call(t, m, me, "GET", path, ""); rec.Code != 404 {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestReplaysDisabled(t *testing.T) {
	me := &auth.Principal{Projects: []auth.Project{{ID: 1}}}
	m := replay.New(replay.Deps{RetentionDays: 7})
	rec := call(t, m, me, "GET", "/v1/replays", "")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"replays":[],"retention_days":7}` {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, m, me, "GET", "/v1/replays/s-1", ""); rec.Code != 404 || strings.TrimSpace(rec.Body.String()) != `{"error":"replay not found"}` {
		t.Errorf("detail: %d %s", rec.Code, rec.Body)
	}
	if rec := call(t, m, nil, "GET", "/v1/replays", ""); rec.Code != 401 {
		t.Errorf("anonymous: %d", rec.Code)
	}
	m = replay.New(replay.Deps{Reader: &fakeReplays{}})
	if rec := call(t, m, me, "GET", "/v1/replays/s-1/events", ""); rec.Code != 503 || !strings.Contains(rec.Body.String(), "storage is not configured") {
		t.Errorf("events without storage: %d %s", rec.Code, rec.Body)
	}
}

func call(t *testing.T, m *replay.Module, p *auth.Principal, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	m.Register(r)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if p != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
