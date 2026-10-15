package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Throttle limits failed logins per email and client address with a Redis
// counter that expires Window after the first failure. It fails open when
// Redis is unavailable.
type Throttle struct {
	Redis  redis.UniversalClient
	Max    int
	Window time.Duration
	// Prefix namespaces the counters; default "login:fail".
	Prefix string
}

func (t *Throttle) key(email, ip string) string {
	p := t.Prefix
	if p == "" {
		p = "login:fail"
	}
	return fmt.Sprintf("%s:%s:%s", p, strings.ToLower(strings.TrimSpace(email)), ip)
}

// Blocked reports whether the pair is locked out and for how long.
func (t *Throttle) Blocked(ctx context.Context, email, ip string) (bool, time.Duration) {
	if t == nil || t.Redis == nil {
		return false, 0
	}
	k := t.key(email, ip)
	n, err := t.Redis.Get(ctx, k).Int()
	if err != nil || n < t.Max {
		return false, 0
	}
	ttl, err := t.Redis.TTL(ctx, k).Result()
	if err != nil || ttl <= 0 {
		ttl = t.Window
	}
	return true, ttl
}

// Fail records a failed attempt.
func (t *Throttle) Fail(ctx context.Context, email, ip string) {
	if t == nil || t.Redis == nil {
		return
	}
	k := t.key(email, ip)
	pipe := t.Redis.TxPipeline()
	pipe.Incr(ctx, k)
	pipe.ExpireNX(ctx, k, t.Window)
	_, _ = pipe.Exec(ctx)
}

// Reset clears the counter after a successful login.
func (t *Throttle) Reset(ctx context.Context, email, ip string) {
	if t == nil || t.Redis == nil {
		return
	}
	t.Redis.Del(ctx, t.key(email, ip))
}
