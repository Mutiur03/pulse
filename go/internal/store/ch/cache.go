package ch

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// CacheTTL is how long ClickHouse keeps the results of cached reads.
const CacheTTL = 60 * time.Second

// cacheRange aligns a range to whole minutes (the start down, the end up),
// so the same insight opened by several people within a minute runs the
// same query and can be answered from the query cache. At most a minute of
// extra data at the start of the range is included.
func cacheRange(from, to time.Time) (time.Time, time.Time) {
	f := from.Truncate(time.Minute)
	t := to.Truncate(time.Minute)
	if t.Before(to) {
		t = t.Add(time.Minute)
	}
	return f, t
}

// HeavyMaxThreads caps the threads of one heavy read (a funnel), so a few
// funnels cannot take every core while people open lists and charts. On an
// 8 vCPU server 3 kept every read under its latency target with 5 users;
// QUERY_HEAVY_MAX_THREADS overrides it (0 = the server default).
var HeavyMaxThreads = 3

func init() {
	if v, err := strconv.Atoi(os.Getenv("QUERY_HEAVY_MAX_THREADS")); err == nil {
		HeavyMaxThreads = v
	}
}

// cached returns a context whose queries use the ClickHouse query cache,
// with heavy reads limited to HeavyMaxThreads. Only aggregate reads (trends
// and funnels) use it; lists stay live.
func cached(ctx context.Context, heavy bool) context.Context {
	s := clickhouse.Settings{
		"use_query_cache": 1,
		"query_cache_ttl": int(CacheTTL.Seconds()),
	}
	if heavy && HeavyMaxThreads > 0 {
		s["max_threads"] = HeavyMaxThreads
	}
	return clickhouse.Context(ctx, clickhouse.WithSettings(s))
}
