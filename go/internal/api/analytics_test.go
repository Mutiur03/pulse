package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

type fakeAnalytics struct {
	persons   ch.PersonFilter
	retention ch.RetentionQuery
	paths     ch.PathsQuery
}

func (f *fakeAnalytics) ListPersons(_ context.Context, q ch.PersonFilter) ([]ch.PersonRow, error) {
	f.persons = q
	return []ch.PersonRow{{Person: "ann", DistinctIDs: []string{"ann", "anon-1"}, Events: 3, Traits: `{"email":"ann@example.com","$lib":"pulse-browser"}`}}, nil
}

func (f *fakeAnalytics) ResolvePerson(_ context.Context, _ uint64, id string) (string, error) {
	if id == "anon-1" {
		return "ann", nil
	}
	return id, nil
}

func (f *fakeAnalytics) GetPerson(_ context.Context, _ uint64, person string, _, _ time.Time) (ch.PersonRow, bool, error) {
	if person != "ann" {
		return ch.PersonRow{}, false, nil
	}
	return ch.PersonRow{Person: "ann", Sessions: 1}, true, nil
}

func (f *fakeAnalytics) PersonSessions(context.Context, uint64, string, time.Time, time.Time, int) ([]ch.PersonSession, error) {
	return []ch.PersonSession{{SessionID: "s1", Pageviews: 2, Replay: 1, Errors: 1}}, nil
}

func (f *fakeAnalytics) PersonEvents(context.Context, uint64, string, time.Time, time.Time, int) ([]ch.EventRecord, error) {
	return []ch.EventRecord{{Event: "$pageview", DistinctID: "anon-1", Properties: `{"$pathname":"/"}`}}, nil
}

func (f *fakeAnalytics) RunRetention(_ context.Context, q ch.RetentionQuery) (ch.RetentionData, error) {
	f.retention = q
	return ch.RetentionData{Sizes: []ch.RetentionCell{{Cohort: q.From.Truncate(24 * time.Hour), Users: 5}}}, nil
}

func (f *fakeAnalytics) RunPaths(_ context.Context, q ch.PathsQuery) ([]ch.PathRow, error) {
	f.paths = q
	return []ch.PathRow{{Path: []string{"/", "/pricing"}, Sessions: 4}}, nil
}

func TestAnalyticsEndpoints(t *testing.T) {
	a := &fakeAnalytics{}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	s := &Server{Traces: &fakeTraces{}, Analytics: a, Now: func() time.Time { return now }}
	member := &auth.Principal{Projects: []auth.Project{{ID: 1, Role: "member"}}}

	rec := call(t, s, member, "GET", "/v1/persons?project_id=1&search=ann", "")
	var list gen.PersonList
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || len(list.Persons) != 1 || list.Persons[0].Traits["email"] != "ann@example.com" || list.Persons[0].Traits["$lib"] != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if a.persons.Search != "ann" || a.persons.To.Sub(a.persons.From) != 30*24*time.Hour {
		t.Errorf("filter %+v", a.persons)
	}

	// A distinct id resolves to its person.
	rec = call(t, s, member, "GET", "/v1/persons/profile?project_id=1&id=anon-1", "")
	var d gen.PersonDetail
	_ = json.Unmarshal(rec.Body.Bytes(), &d)
	if rec.Code != 200 || d.Person.Id != "ann" || len(d.Sessions) != 1 || !d.Sessions[0].HasReplay || len(d.Events) != 1 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for path, want := range map[string]int{
		"/v1/persons/profile?project_id=1&id=nobody": 404,
		"/v1/persons/profile?project_id=1&id=":       400,
		"/v1/persons/profile?project_id=2&id=ann":    404,
	} {
		if rec := call(t, s, member, "GET", path, ""); rec.Code != want {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}

	rec = call(t, s, member, "POST", "/v1/insights/retention?project_id=1", `{"start_event":"signup","period":"week","periods":4}`)
	var ret gen.RetentionResult
	_ = json.Unmarshal(rec.Body.Bytes(), &ret)
	if rec.Code != 200 || ret.Period != "week" || len(ret.Cohorts) < 8 || a.retention.StartEvent != "signup" || a.retention.To.Sub(a.retention.From) != 56*24*time.Hour {
		t.Fatalf("%d %s %+v", rec.Code, rec.Body, a.retention)
	}
	for _, b := range []string{`{"period":"month"}`, `{"periods":99}`, `nope`} {
		if rec := call(t, s, member, "POST", "/v1/insights/retention?project_id=1", b); rec.Code != 400 {
			t.Errorf("%s: %d", b, rec.Code)
		}
	}

	rec = call(t, s, member, "POST", "/v1/insights/paths?project_id=1", `{"start":"/","steps":3}`)
	var paths gen.PathsResult
	_ = json.Unmarshal(rec.Body.Bytes(), &paths)
	if rec.Code != 200 || paths.TotalSessions != 4 || len(paths.Links) != 1 || paths.Mode != "pageview" || a.paths.Start != "/" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, b := range []string{`{"mode":"clicks"}`, `{"steps":1}`} {
		if rec := call(t, s, member, "POST", "/v1/insights/paths?project_id=1", b); rec.Code != 400 {
			t.Errorf("%s: %d", b, rec.Code)
		}
	}
}
