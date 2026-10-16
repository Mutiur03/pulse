package ingest

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/Mutiur03/pulse/internal/keys"
)

var _ OriginChecker = (*KeyAuth)(nil)

// KeyAuth adapts the keys package to the ingest Authenticator interface.
// Rate limits count requests per key per second.
type KeyAuth struct {
	Resolver *keys.Resolver
	Limiter  *keys.Limiter
}

func (a *KeyAuth) Authenticate(ctx context.Context, key string) (string, error) {
	k, err := a.Resolver.Resolve(ctx, key)
	if errors.Is(err, keys.ErrInvalidKey) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(k.ProjectID, 10), nil
}

func (a *KeyAuth) Allow(ctx context.Context, key string) (bool, time.Duration) {
	k, err := a.Resolver.Resolve(ctx, key) // served from cache
	if err != nil {
		return true, 0
	}
	return a.Limiter.Allow(ctx, key, k.RateLimitPerSec, 1)
}

// OriginAllowed checks the browser origin against the project's allowed
// origins. Unknown keys pass here; Authenticate rejects them.
func (a *KeyAuth) OriginAllowed(ctx context.Context, key, origin string) bool {
	k, err := a.Resolver.Resolve(ctx, key) // served from cache
	if err != nil {
		return true
	}
	return keys.OriginAllowed(k.Origins, origin)
}
