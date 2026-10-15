package alerts

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLocker takes leases with SET NX.
type RedisLocker struct{ Client *redis.Client }

func (l RedisLocker) Lock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if ttl < time.Second {
		ttl = time.Second
	}
	return l.Client.SetNX(ctx, key, 1, ttl).Result()
}
