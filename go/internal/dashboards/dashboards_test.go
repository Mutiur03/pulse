package dashboards

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	d := Dashboard{Name: "  Growth ", Widgets: []Widget{{ID: "w1", Kind: KindTrend, Width: 5}, {ID: "w2", Kind: KindTraces, Query: json.RawMessage(`{"service":"api"}`)}}}
	var checked []string
	err := Clean(&d, func(kind string, q json.RawMessage) error {
		checked = append(checked, kind+string(q))
		return nil
	})
	if err != nil || d.Name != "Growth" || d.Widgets[0].Width != 1 || string(d.Widgets[0].Query) != "{}" || len(checked) != 2 {
		t.Fatalf("%v %+v %v", err, d, checked)
	}
	empty := Dashboard{Name: "x"}
	if err := Clean(&empty, nil); err != nil || empty.Widgets == nil {
		t.Errorf("nil widgets become empty: %v", err)
	}
	many := make([]Widget, MaxWidgets+1)
	for i := range many {
		many[i] = Widget{ID: strings.Repeat("a", i+1), Kind: KindMetric}
	}
	for name, bad := range map[string]Dashboard{
		"no name":     {Name: " "},
		"long name":   {Name: strings.Repeat("n", 121)},
		"bad kind":    {Name: "x", Widgets: []Widget{{ID: "a", Kind: "pie"}}},
		"dup id":      {Name: "x", Widgets: []Widget{{ID: "a", Kind: KindMetric}, {ID: "a", Kind: KindMetric}}},
		"no id":       {Name: "x", Widgets: []Widget{{Kind: KindMetric}}},
		"too many":    {Name: "x", Widgets: many},
		"large query": {Name: "x", Widgets: []Widget{{ID: "a", Kind: KindMetric, Query: json.RawMessage(`"` + strings.Repeat("q", MaxWidgetBytes) + `"`)}}},
	} {
		if err := Clean(&bad, nil); err == nil {
			t.Errorf("%s should fail", name)
		}
	}
	failing := Dashboard{Name: "x", Widgets: []Widget{{ID: "a", Title: "Revenue", Kind: KindFunnel}}}
	if err := Clean(&failing, func(string, json.RawMessage) error { return errors.New("needs steps") }); err == nil || !strings.Contains(err.Error(), `"Revenue": needs steps`) {
		t.Errorf("check error %v", err)
	}
}

func TestCanEdit(t *testing.T) {
	private := Dashboard{CreatedBy: 1}
	shared := Dashboard{CreatedBy: 1, Shared: true}
	cases := []struct {
		d     Dashboard
		user  int64
		owner bool
		want  bool
	}{{private, 1, false, true}, {private, 2, true, false}, {shared, 2, false, false}, {shared, 2, true, true}}
	for i, c := range cases {
		if got := CanEdit(c.d, c.user, c.owner); got != c.want {
			t.Errorf("case %d: %v", i, got)
		}
	}
}
