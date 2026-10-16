// Package domain defines dashboards: named grids of saved queries
// (widgets) that are private to their creator or shared with everyone who
// can read the project.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNotFound means the dashboard does not exist or the user cannot see it.
var ErrNotFound = errors.New("dashboard not found")

// Widget kinds.
const (
	KindTrend  = "trend"
	KindFunnel = "funnel"
	KindMetric = "metric"
	KindTraces = "traces"
)

// Limits on a dashboard.
const (
	MaxWidgets     = 30
	MaxWidgetBytes = 8 << 10
)

// Widget is a saved query shown on a dashboard. Query holds the request of
// its kind: a trend query, funnel steps, a metric query or trace filters.
type Widget struct {
	ID    string          `json:"id"`
	Title string          `json:"title"`
	Kind  string          `json:"kind"`
	Width int             `json:"width"`
	Query json.RawMessage `json:"query"`
}

// Dashboard is a dashboard with its widgets.
type Dashboard struct {
	ID          int64
	ProjectID   int64
	Name        string
	Description string
	CreatedBy   int64
	CreatorName string
	Shared      bool
	Widgets     []Widget
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// QueryCheck validates a widget's query for its kind.
type QueryCheck func(kind string, query json.RawMessage) error

// Clean validates and normalizes a dashboard's fields and widgets.
func Clean(d *Dashboard, check QueryCheck) error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || utf8.RuneCountInString(d.Name) > 120 {
		return errors.New("dashboard name must be 1 to 120 characters")
	}
	d.Description = strings.TrimSpace(d.Description)
	if utf8.RuneCountInString(d.Description) > 500 {
		return errors.New("description must be at most 500 characters")
	}
	if len(d.Widgets) > MaxWidgets {
		return fmt.Errorf("a dashboard holds at most %d widgets", MaxWidgets)
	}
	seen := map[string]bool{}
	for i := range d.Widgets {
		w := &d.Widgets[i]
		w.ID = strings.TrimSpace(w.ID)
		if w.ID == "" || len(w.ID) > 40 || seen[w.ID] {
			return errors.New("each widget needs a unique id of at most 40 characters")
		}
		seen[w.ID] = true
		w.Title = strings.TrimSpace(w.Title)
		if utf8.RuneCountInString(w.Title) > 120 {
			return errors.New("widget title must be at most 120 characters")
		}
		switch w.Kind {
		case KindTrend, KindFunnel, KindMetric, KindTraces:
		default:
			return fmt.Errorf("unknown widget kind %q", w.Kind)
		}
		if w.Width != 2 {
			w.Width = 1
		}
		if len(w.Query) == 0 || string(w.Query) == "null" {
			w.Query = json.RawMessage("{}")
		}
		if len(w.Query) > MaxWidgetBytes {
			return errors.New("widget query is too large")
		}
		if check != nil {
			if err := check(w.Kind, w.Query); err != nil {
				label := w.Title
				if label == "" {
					label = w.Kind
				}
				return fmt.Errorf("widget %q: %w", label, err)
			}
		}
	}
	if d.Widgets == nil {
		d.Widgets = []Widget{}
	}
	return nil
}

// CanEdit reports whether a user may change a dashboard: its creator, or a
// project owner when the dashboard is shared.
func CanEdit(d Dashboard, user int64, projectOwner bool) bool {
	return d.CreatedBy == user || (d.Shared && projectOwner)
}
