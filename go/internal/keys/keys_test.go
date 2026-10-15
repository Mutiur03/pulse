package keys

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type fakeLookup struct {
	keys  map[string]Key
	calls int
}

func (f *fakeLookup) Find(_ context.Context, k string) (Key, error) {
	f.calls++
	if v, ok := f.keys[k]; ok {
		return v, nil
	}
	return Key{}, ErrInvalidKey
}

func setup(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	mr := miniredis.RunT(t)
	return mr, redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

const good = "pk_0123456789abcdef0123"

func TestResolveCachesHitsAndMisses(t *testing.T) {
	mr, rdb := setup(t)
	src := &fakeLookup{keys: map[string]Key{good: {ProjectID: 7, RateLimitPerSec: 50}}}
	r := &Resolver{Redis: rdb, Source: src}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		k, err := r.Resolve(ctx, good)
		if err != nil || k.ProjectID != 7 || k.RateLimitPerSec != 50 {
			t.Fatalf("resolve: %+v %v", k, err)
		}
	}
	if src.calls != 1 {
		t.Fatalf("want 1 source lookup, got %d", src.calls)
	}

	bad := "pk_unknownunknownunknown"
	for i := 0; i < 3; i++ {
		if _, err := r.Resolve(ctx, bad); err != ErrInvalidKey {
			t.Fatalf("want invalid, got %v", err)
		}
	}
	if src.calls != 2 {
		t.Fatalf("negative result should be cached, got %d lookups", src.calls)
	}

	// Revocation: once the cache entry expires (or is purged) the key stops working.
	delete(src.keys, good)
	if err := Purge(ctx, rdb, good); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(ctx, good); err != ErrInvalidKey {
		t.Fatalf("purged revoked key should be rejected, got %v", err)
	}
	mr.FastForward(NegativeTTL + time.Second)
	if mr.Exists(cachePrefix + bad) {
		t.Error("negative cache entry should expire")
	}
}

func TestResolveRejectsMalformedWithoutLookup(t *testing.T) {
	src := &fakeLookup{}
	r := &Resolver{Source: src}
	for _, k := range []string{"", "pk_short", "sk_0123456789abcdef0123", "pk_0123456789abcdef'; DROP", "pk_" + string(make([]byte, 200))} {
		if _, err := r.Resolve(context.Background(), k); err != ErrInvalidKey {
			t.Errorf("%q: want invalid", k)
		}
	}
	if src.calls != 0 {
		t.Errorf("malformed keys must not reach the store, got %d", src.calls)
	}
}

func TestResolveWorksWithoutRedis(t *testing.T) {
	mr, rdb := setup(t)
	mr.Close() // Redis down
	r := &Resolver{Redis: rdb, Source: &fakeLookup{keys: map[string]Key{good: {ProjectID: 1, RateLimitPerSec: 1}}}}
	if k, err := r.Resolve(context.Background(), good); err != nil || k.ProjectID != 1 {
		t.Fatalf("should fall back to the source: %+v %v", k, err)
	}
}

func TestLimiterFixedWindow(t *testing.T) {
	_, rdb := setup(t)
	now := time.Date(2026, 10, 15, 12, 0, 0, 100_000_000, time.UTC)
	l := &Limiter{Redis: rdb, Now: func() time.Time { return now }}
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if ok, _ := l.Allow(ctx, good, 10, 2); !ok {
			t.Fatalf("request %d within budget was limited", i)
		}
	}
	ok, retry := l.Allow(ctx, good, 10, 1)
	if ok {
		t.Fatal("11th record in the same second should be limited")
	}
	if retry <= 0 || retry > time.Second {
		t.Errorf("retry-after %v", retry)
	}
	if ok, _ := l.Allow(ctx, "pk_otherkeyotherkey00", 10, 1); !ok {
		t.Error("limits are per key")
	}
	now = now.Add(time.Second)
	if ok, _ := l.Allow(ctx, good, 10, 10); !ok {
		t.Error("budget should reset in the next second")
	}
	if ok, _ := l.Allow(ctx, good, 10, 11); ok {
		t.Error("a single batch larger than the budget is limited")
	}
}

func TestLimiterFailsOpen(t *testing.T) {
	mr, rdb := setup(t)
	mr.Close()
	if ok, _ := (&Limiter{Redis: rdb}).Allow(context.Background(), good, 1, 100); !ok {
		t.Error("limiter must fail open when Redis is down")
	}
}
