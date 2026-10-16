package governance

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

type fakeStore struct {
	queue    []Request
	finished []Request
	settings []Settings
}

func (f *fakeStore) Claim(context.Context) (Request, bool, error) {
	if len(f.queue) == 0 {
		return Request{}, false, nil
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	r.Attempts++
	return r, true, nil
}

func (f *fakeStore) Finish(_ context.Context, id int64, ids []string, result map[string]int64, err error) (Request, error) {
	r := Request{ID: id, DistinctIDs: ids, Result: result, Status: Completed}
	if err != nil {
		r.Status, r.Error = Failed, err.Error()
	}
	f.finished = append(f.finished, r)
	return r, nil
}

func (f *fakeStore) AllSettings(context.Context) ([]Settings, error) { return f.settings, nil }

type fakeCH struct {
	deleted map[string]time.Time
	failOn  string
}

func (f *fakeCH) LinkedIDs(_ context.Context, _ uint64, ids []string) ([]string, error) {
	out := append([]string{}, ids...)
	for _, id := range ids {
		if id == "anon-1" {
			out = append(out, "user-1")
		}
	}
	return out, nil
}

func (f *fakeCH) FindPersonData(context.Context, uint64, []string) (ch.PersonData, error) {
	return ch.PersonData{Sessions: []string{"s1"}, ChunkKeys: []string{"replays/7/s1/1-a.json.gz"}}, nil
}

func (f *fakeCH) DeletePersonData(context.Context, uint64, []string, ch.PersonData) (map[string]int64, error) {
	if f.failOn == "delete" {
		return map[string]int64{"events": 3}, errors.New("clickhouse down")
	}
	return map[string]int64{"events": 3, "person_aliases": 2}, nil
}

func (f *fakeCH) DeleteOlder(_ context.Context, _ uint64, kind string, cutoff time.Time) (int64, error) {
	if f.deleted == nil {
		f.deleted = map[string]time.Time{}
	}
	f.deleted[kind] = cutoff
	return 5, nil
}

type fakeBucket struct {
	objs    []replay.Object
	removed []string
}

func (b *fakeBucket) List(_ context.Context, prefix string) ([]replay.Object, error) {
	var out []replay.Object
	for _, o := range b.objs {
		if len(o.Key) >= len(prefix) && o.Key[:len(prefix)] == prefix {
			out = append(out, o)
		}
	}
	return out, nil
}

func (b *fakeBucket) Remove(_ context.Context, key string) error {
	b.removed = append(b.removed, key)
	return nil
}

type fakeAudit struct{ entries []audit.Entry }

func (a *fakeAudit) Record(_ context.Context, e audit.Entry) error {
	a.entries = append(a.entries, e)
	return nil
}

type fakeArtifacts struct{ cutoff time.Time }

func (f *fakeArtifacts) ExpireArtifacts(_ context.Context, _ int64, cutoff time.Time) ([]string, int, error) {
	f.cutoff = cutoff
	return []string{"sourcemaps/7/ab/abc.map"}, 2, nil
}

func TestRunDeletions(t *testing.T) {
	st := &fakeStore{queue: []Request{{ID: 1, ProjectID: 7, DistinctID: "anon-1", PersonKey: "user-1"}}}
	bucket := &fakeBucket{objs: []replay.Object{{Key: "replays/7/s1/1-a.json.gz"}, {Key: "replays/7/s1/2-b.json.gz"}, {Key: "replays/7/s2/1-c.json.gz"}}}
	au := &fakeAudit{}
	w := &Worker{Store: st, Analytics: &fakeCH{}, Replays: bucket, Audit: au}
	n, err := w.RunDeletions(context.Background())
	if err != nil || n != 1 || len(st.finished) != 1 {
		t.Fatalf("%d %v %+v", n, err, st.finished)
	}
	r := st.finished[0]
	sort.Strings(bucket.removed)
	if r.Status != Completed || r.Result["events"] != 3 || r.Result["replay_objects"] != 2 || len(bucket.removed) != 2 || bucket.removed[1] != "replays/7/s1/2-b.json.gz" {
		t.Fatalf("result %+v removed %v", r, bucket.removed)
	}
	if len(au.entries) != 1 || au.entries[0].Action != ActionDeletionComplete || au.entries[0].ProjectID != 7 {
		t.Fatalf("audit %+v", au.entries)
	}

	// A failure is reported to the store, which decides about retrying.
	st.queue = []Request{{ID: 2, ProjectID: 7, DistinctID: "x"}}
	w.Analytics = &fakeCH{failOn: "delete"}
	if _, err := w.RunDeletions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f := st.finished[1]; f.Status != Failed || f.Error != "clickhouse down" || au.entries[1].Action != ActionDeletionFailed {
		t.Fatalf("failure %+v %+v", f, au.entries)
	}
}

func TestEnforceRetention(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	short := DefaultRetention()
	short.EventsDays, short.ReplaysDays, short.SourceMapsDays = 7, 2, 30
	st := &fakeStore{settings: []Settings{Defaults(1), {ProjectID: 7, Retention: short, Saved: true}}}
	bucket := &fakeBucket{objs: []replay.Object{
		{Key: "replays/1/s/old.json.gz", Modified: now.Add(-40 * 24 * time.Hour)},
		{Key: "replays/1/s/new.json.gz", Modified: now.Add(-time.Hour)},
		{Key: "replays/7/s/3d.json.gz", Modified: now.Add(-3 * 24 * time.Hour)},
	}}
	maps := &fakeBucket{}
	chf, arts, au := &fakeCH{}, &fakeArtifacts{}, &fakeAudit{}
	w := &Worker{Store: st, Analytics: chf, Replays: bucket, SourceMaps: maps, Artifacts: arts, Audit: au, Now: func() time.Time { return now }}
	out, err := w.EnforceRetention(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Only shortened periods run deletes; caps are left to the table TTL.
	if len(chf.deleted) != 2 || !chf.deleted["events"].Equal(now.Add(-7*24*time.Hour)) || !chf.deleted["replays"].Equal(now.Add(-2*24*time.Hour)) {
		t.Fatalf("deleted %v", chf.deleted)
	}
	sort.Strings(bucket.removed)
	if len(bucket.removed) != 2 || bucket.removed[0] != "replays/1/s/old.json.gz" || bucket.removed[1] != "replays/7/s/3d.json.gz" {
		t.Fatalf("objects %v", bucket.removed)
	}
	if !arts.cutoff.Equal(now.Add(-30*24*time.Hour)) || len(maps.removed) != 1 {
		t.Fatalf("source maps %v %v", arts.cutoff, maps.removed)
	}
	if out[7]["events"] != 5 || out[7]["source_maps"] != 2 || out[1]["replay_objects"] != 1 || len(au.entries) != 2 {
		t.Fatalf("out %v audit %d", out, len(au.entries))
	}
}

func TestRetentionValidate(t *testing.T) {
	if err := DefaultRetention().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Retention{
		{EventsDays: 31, TracesDays: 7, LogsDays: 3, ErrorsDays: 90, ReplaysDays: 30},
		{EventsDays: 30, TracesDays: 0, LogsDays: 3, ErrorsDays: 90, ReplaysDays: 30},
		{EventsDays: 30, TracesDays: 7, LogsDays: 3, ErrorsDays: 90, ReplaysDays: 30, SourceMapsDays: -1},
	} {
		if r.Validate() == nil {
			t.Errorf("accepted %+v", r)
		}
	}
}
