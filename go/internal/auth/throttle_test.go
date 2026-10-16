package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestThrottle(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()
	th := &Throttle{Redis: redis.NewClient(&redis.Options{Addr: mr.Addr()}), Max: 2, Window: time.Minute}

	if b, _ := th.Blocked(ctx, "A@x.io "); b {
		t.Fatal("blocked before any failure")
	}
	th.Fail(ctx, "a@x.io")
	th.Fail(ctx, " A@X.IO")
	b, wait := th.Blocked(ctx, "a@x.io")
	if !b || wait <= 0 || wait > time.Minute {
		t.Fatalf("blocked=%v wait=%v", b, wait)
	}
	if !mr.Exists("login:fail:a@x.io") {
		t.Fatalf("keys %v", mr.Keys())
	}
	th.Reset(ctx, "a@x.io")
	if b, _ := th.Blocked(ctx, "a@x.io"); b {
		t.Fatal("blocked after reset")
	}

	sig := &Throttle{Redis: th.Redis, Max: 1, Window: time.Minute, Prefix: "signup"}
	if ok, _ := sig.Take(ctx, "1.2.3.4"); !ok {
		t.Fatal("first take refused")
	}
	if ok, wait := sig.Take(ctx, "1.2.3.4"); ok || wait <= 0 {
		t.Fatalf("second take ok=%v wait=%v", ok, wait)
	}

	// Disabled or unreachable throttles fail open.
	var off *Throttle
	off.Fail(ctx, "x")
	off.Reset(ctx, "x")
	if b, _ := off.Blocked(ctx, "x"); b {
		t.Fatal("nil throttle blocks")
	}
	if ok, _ := off.Take(ctx, "x"); !ok {
		t.Fatal("nil throttle refuses")
	}
	mr.Close()
	if ok, _ := sig.Take(ctx, "5.6.7.8"); !ok {
		t.Fatal("throttle without redis refuses")
	}
}
