package httpapi

import (
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/governance"
)

func rulesOut(r governance.Rules) gen.ScrubRules {
	keys, patterns := r.Keys, r.Patterns
	if keys == nil {
		keys = []string{}
	}
	if patterns == nil {
		patterns = []string{}
	}
	return gen.ScrubRules{Emails: r.Emails, Cards: r.Cards, Ips: r.IPs, Keys: keys, Patterns: patterns}
}

func rulesIn(r gen.ScrubRules) governance.Rules {
	return governance.Rules{Emails: r.Emails, Cards: r.Cards, IPs: r.Ips, Keys: r.Keys, Patterns: r.Patterns}
}

func retentionOut(r governance.Retention) gen.RetentionSettings {
	return gen.RetentionSettings{EventsDays: r.EventsDays, TracesDays: r.TracesDays, LogsDays: r.LogsDays,
		ErrorsDays: r.ErrorsDays, ReplaysDays: r.ReplaysDays, SourcemapsDays: r.SourceMapsDays}
}

func retentionIn(r gen.RetentionSettings) governance.Retention {
	return governance.Retention{EventsDays: r.EventsDays, TracesDays: r.TracesDays, LogsDays: r.LogsDays,
		ErrorsDays: r.ErrorsDays, ReplaysDays: r.ReplaysDays, SourceMapsDays: r.SourcemapsDays}
}

func settingsOut(st governance.Settings) gen.DataSettings {
	out := gen.DataSettings{ProjectId: st.ProjectID, Scrubbing: rulesOut(st.Rules), Retention: retentionOut(st.Retention),
		Limits: retentionOut(governance.Caps), Saved: st.Saved}
	if st.Saved {
		t := st.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

func requestOut(r governance.Request) gen.DeletionRequest {
	ids := r.DistinctIDs
	if ids == nil {
		ids = []string{}
	}
	result := r.Result
	if result == nil {
		result = map[string]int64{}
	}
	return gen.DeletionRequest{Id: r.ID, ProjectId: r.ProjectID, DistinctId: r.DistinctID, PersonKey: r.PersonKey,
		DistinctIds: ids, Reason: r.Reason, Status: gen.DeletionStatus(r.Status), Result: result, Error: r.Error,
		Attempts: r.Attempts, CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, CompletedAt: r.CompletedAt}
}
