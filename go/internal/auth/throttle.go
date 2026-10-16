package auth

import (
	"context"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Throttle is a fixed-window counter in Redis keyed by any parts (an email,
// a client address, or both). A window starts at the first hit and lasts
// Window. It fails open when Redis is unavailable.
type Throttle struct {
	Redis  redis.UniversalClient
	Max    int
	Window time.Duration
	// Prefix namespaces the counters; default "login:fail".
	Prefix string
}

func (t *Throttle) key(parts []string) string {
	p := t.Prefix
	if p == "" {
		p = "login:fail"
	}
	var b strings.Builder
	b.WriteString(p)
	for _, s := range parts {
		b.WriteByte(':')
		b.WriteString(strings.ToLower(strings.TrimSpace(s)))
	}
	return b.String()
}

func (t *Throttle) off() bool { return t == nil || t.Redis == nil || t.Max <= 0 }

func (t *Throttle) wait(ctx context.Context, k string) time.Duration {
	ttl, err := t.Redis.TTL(ctx, k).Result()
	if err != nil || ttl <= 0 {
		return t.Window
	}
	return ttl
}

// Blocked reports whether the counter is at its limit and for how long.
func (t *Throttle) Blocked(ctx context.Context, parts ...string) (bool, time.Duration) {
	if t.off() {
		return false, 0
	}
	k := t.key(parts)
	n, err := t.Redis.Get(ctx, k).Int()
	if err != nil || n < t.Max {
		return false, 0
	}
	return true, t.wait(ctx, k)
}

// Fail records one hit, e.g. a failed attempt.
func (t *Throttle) Fail(ctx context.Context, parts ...string) {
	if t.off() {
		return
	}
	k := t.key(parts)
	pipe := t.Redis.TxPipeline()
	pipe.Incr(ctx, k)
	pipe.ExpireNX(ctx, k, t.Window)
	_, _ = pipe.Exec(ctx)
}

// Take counts one hit and reports whether it was over the limit, for limits
// on every attempt rather than only failed ones.
func (t *Throttle) Take(ctx context.Context, parts ...string) (bool, time.Duration) {
	if t.off() {
		return true, 0
	}
	k := t.key(parts)
	pipe := t.Redis.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.ExpireNX(ctx, k, t.Window)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, 0
	}
	if incr.Val() > int64(t.Max) {
		return false, t.wait(ctx, k)
	}
	return true, 0
}

// Reset clears the counter, e.g. after a successful login.
func (t *Throttle) Reset(ctx context.Context, parts ...string) {
	if t.off() {
		return
	}
	t.Redis.Del(ctx, t.key(parts))
}
