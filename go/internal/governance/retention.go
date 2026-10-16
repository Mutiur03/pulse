package governance

import (
	"errors"
	"fmt"
	"time"
)

// Retention is how many days each kind of data is kept.
type Retention struct {
	EventsDays     int // product events and sessions
	TracesDays     int // spans
	LogsDays       int
	ErrorsDays     int
	ReplaysDays    int // replay rows and their objects
	SourceMapsDays int // 0 keeps source maps until deleted
}

// Caps are the longest periods: the ClickHouse table TTLs. Projects can
// keep data for less, not more. Source maps have no table TTL.
var Caps = Retention{EventsDays: 30, TracesDays: 7, LogsDays: 3, ErrorsDays: 90, ReplaysDays: 30, SourceMapsDays: 3650}

// DefaultRetention keeps everything for the full cap and source maps
// until they are deleted.
func DefaultRetention() Retention {
	r := Caps
	r.SourceMapsDays = 0
	return r
}

// Validate checks each period against its cap.
func (r Retention) Validate() error {
	for _, f := range []struct {
		name     string
		v, max   int
		zeroOkay bool
	}{
		{"events", r.EventsDays, Caps.EventsDays, false},
		{"traces", r.TracesDays, Caps.TracesDays, false},
		{"logs", r.LogsDays, Caps.LogsDays, false},
		{"errors", r.ErrorsDays, Caps.ErrorsDays, false},
		{"replays", r.ReplaysDays, Caps.ReplaysDays, false},
		{"source maps", r.SourceMapsDays, Caps.SourceMapsDays, true},
	} {
		if (f.v < 1 && !(f.zeroOkay && f.v == 0)) || f.v > f.max {
			if f.zeroOkay {
				return fmt.Errorf("%s retention must be 0 (keep) or 1 to %d days", f.name, f.max)
			}
			return fmt.Errorf("%s retention must be 1 to %d days", f.name, f.max)
		}
	}
	return nil
}

// Settings are a project's data rules.
type Settings struct {
	ProjectID int64
	Rules     Rules
	Retention Retention
	// Saved is false when the project uses the defaults.
	Saved     bool
	UpdatedAt time.Time
}

// Defaults returns the settings of a project that saved none.
func Defaults(project int64) Settings {
	return Settings{ProjectID: project, Rules: DefaultRules(), Retention: DefaultRetention()}
}

// Validate cleans the rules and checks retention.
func (s *Settings) Validate() error {
	r, err := s.Rules.Clean()
	if err != nil {
		return err
	}
	s.Rules = r
	return s.Retention.Validate()
}

// Cutoff is the oldest time kept for a period of days at now.
func Cutoff(now time.Time, days int) time.Time { return now.Add(-time.Duration(days) * 24 * time.Hour) }

// ErrNotFound is returned for unknown deletion requests.
var ErrNotFound = errors.New("not found")
