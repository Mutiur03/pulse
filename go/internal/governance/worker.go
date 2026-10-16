package governance

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/monitor"
	"github.com/Mutiur03/pulse/internal/replay"
	"github.com/Mutiur03/pulse/internal/store/ch"
)

// Analytics is the ClickHouse side of deletions and retention.
type Analytics interface {
	LinkedIDs(ctx context.Context, project uint64, ids []string) ([]string, error)
	FindPersonData(ctx context.Context, project uint64, ids []string) (ch.PersonData, error)
	DeletePersonData(ctx context.Context, project uint64, ids []string, d ch.PersonData) (map[string]int64, error)
	DeleteOlder(ctx context.Context, project uint64, kind string, cutoff time.Time) (int64, error)
}

// Objects is a bucket.
type Objects interface {
	List(ctx context.Context, prefix string) ([]replay.Object, error)
	Remove(ctx context.Context, key string) error
}

// Artifacts expires source maps.
type Artifacts interface {
	ExpireArtifacts(ctx context.Context, project int64, cutoff time.Time) ([]string, int, error)
}

// Requests is the Postgres side.
type Requests interface {
	Claim(ctx context.Context) (Request, bool, error)
	Finish(ctx context.Context, id int64, ids []string, result map[string]int64, runErr error) (Request, error)
	AllSettings(ctx context.Context) ([]Settings, error)
}

// Audit actions written by the worker.
const (
	ActionDeletionRequest  = "data.deletion.request"
	ActionDeletionComplete = "data.deletion.complete"
	ActionDeletionFailed   = "data.deletion.failed"
	ActionSettingsUpdate   = "data.settings.update"
	ActionRetention        = "data.retention"
)

// Worker runs deletion requests and enforces retention. Replays and
// SourceMaps may be nil when object storage is not configured.
type Worker struct {
	Store      Requests
	Analytics  Analytics
	Replays    Objects
	SourceMaps Objects
	Artifacts  Artifacts
	Audit      audit.Recorder
	Now        func() time.Time
}

func (w *Worker) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now().UTC()
}

func (w *Worker) record(ctx context.Context, e audit.Entry) {
	if w.Audit == nil {
		return
	}
	if err := w.Audit.Record(ctx, e); err != nil {
		log.Printf("audit: %v", err)
	}
}

// RunDeletions processes queued requests until none is left and returns
// how many it handled.
func (w *Worker) RunDeletions(ctx context.Context) (int, error) {
	n := 0
	for {
		r, ok, err := w.Store.Claim(ctx)
		if err != nil || !ok {
			return n, err
		}
		n++
		ids, result, runErr := w.Delete(ctx, r)
		done, err := w.Store.Finish(ctx, r.ID, ids, result, runErr)
		if err != nil {
			return n, err
		}
		monitor.GovernanceDeletions.WithLabelValues(done.Status).Inc()
		details := map[string]any{"request_id": r.ID, "ids": len(ids)}
		for k, v := range result {
			details[k] = v
		}
		switch {
		case runErr == nil:
			w.record(ctx, audit.Entry{ProjectID: r.ProjectID, Action: ActionDeletionComplete, Target: r.DistinctID, Details: details})
		case done.Status == Failed:
			details["error"] = runErr.Error()
			w.record(ctx, audit.Entry{ProjectID: r.ProjectID, Action: ActionDeletionFailed, Target: r.DistinctID, Details: details})
		default:
			log.Printf("deletion request %d failed, will retry: %v", r.ID, runErr)
		}
	}
}

// Delete removes everything of the request's person: rows in every
// ClickHouse table and replay objects. It is safe to run again.
func (w *Worker) Delete(ctx context.Context, r Request) ([]string, map[string]int64, error) {
	project := uint64(r.ProjectID) //nolint:gosec // project ids are positive
	start := append(append([]string{}, r.DistinctIDs...), r.DistinctID, r.PersonKey)
	ids, err := w.Analytics.LinkedIDs(ctx, project, start)
	if err != nil {
		return nil, nil, fmt.Errorf("linked ids: %w", err)
	}
	data, err := w.Analytics.FindPersonData(ctx, project, ids)
	if err != nil {
		return ids, nil, fmt.Errorf("find data: %w", err)
	}
	// Objects first: if a later step fails the rows that point at them are
	// still there and the retry finds them again.
	removed := int64(0)
	if w.Replays != nil {
		keys := map[string]bool{}
		for _, k := range data.ChunkKeys {
			keys[k] = true
		}
		for _, s := range data.Sessions {
			objs, err := w.Replays.List(ctx, fmt.Sprintf("replays/%d/%s/", project, s))
			if err != nil {
				return ids, nil, fmt.Errorf("list replays: %w", err)
			}
			for _, o := range objs {
				keys[o.Key] = true
			}
		}
		for k := range keys {
			if err := w.Replays.Remove(ctx, k); err != nil {
				return ids, nil, fmt.Errorf("remove replay: %w", err)
			}
			removed++
		}
	}
	result, err := w.Analytics.DeletePersonData(ctx, project, ids, data)
	if result == nil {
		result = map[string]int64{}
	}
	result["replay_objects"] = removed
	result["sessions_found"] = int64(len(data.Sessions))
	return ids, result, err
}

// kinds pairs each retention period with its data kind.
func kinds(r Retention) []struct {
	kind      string
	days, cap int
} {
	return []struct {
		kind      string
		days, cap int
	}{
		{"events", r.EventsDays, Caps.EventsDays},
		{"traces", r.TracesDays, Caps.TracesDays},
		{"logs", r.LogsDays, Caps.LogsDays},
		{"errors", r.ErrorsDays, Caps.ErrorsDays},
		{"replays", r.ReplaysDays, Caps.ReplaysDays},
	}
}

// EnforceRetention deletes data older than each project's periods. Table
// TTLs already drop data past the caps; this removes what a project keeps
// for less, plus replay objects and expired source maps, which have no TTL.
func (w *Worker) EnforceRetention(ctx context.Context) (map[int64]map[string]int64, error) {
	all, err := w.Store.AllSettings(ctx)
	if err != nil {
		return nil, err
	}
	now := w.now()
	out := map[int64]map[string]int64{}
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for _, s := range all {
		project := uint64(s.ProjectID) //nolint:gosec // project ids are positive
		counts := map[string]int64{}
		for _, k := range kinds(s.Retention) {
			if k.days <= 0 || k.days >= k.cap {
				continue
			}
			n, err := w.Analytics.DeleteOlder(ctx, project, k.kind, Cutoff(now, k.days))
			keep(err)
			if n > 0 {
				counts[k.kind] = n
			}
		}
		if w.Replays != nil {
			cutoff := Cutoff(now, s.Retention.ReplaysDays)
			objs, err := w.Replays.List(ctx, fmt.Sprintf("replays/%d/", project))
			keep(err)
			for _, o := range objs {
				if o.Modified.Before(cutoff) {
					if err := w.Replays.Remove(ctx, o.Key); err != nil {
						keep(err)
						break
					}
					counts["replay_objects"]++
				}
			}
		}
		if s.Retention.SourceMapsDays > 0 && w.Artifacts != nil {
			keys, n, err := w.Artifacts.ExpireArtifacts(ctx, s.ProjectID, Cutoff(now, s.Retention.SourceMapsDays))
			keep(err)
			if n > 0 {
				counts["source_maps"] = int64(n)
			}
			for _, k := range keys {
				if w.SourceMaps != nil {
					keep(w.SourceMaps.Remove(ctx, k))
				}
			}
		}
		if len(counts) > 0 {
			out[s.ProjectID] = counts
			details := map[string]any{}
			for k, v := range counts {
				details[k] = v
				monitor.GovernanceRetainedRows.WithLabelValues(k).Add(float64(v))
			}
			w.record(ctx, audit.Entry{ProjectID: s.ProjectID, Action: ActionRetention, Target: "retention", Details: details})
		}
	}
	return out, firstErr
}
