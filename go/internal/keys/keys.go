// Package keys resolves public ingest keys to projects, with a Redis cache
// in front of Postgres, and applies per-key rate limits.
package keys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// ErrInvalidKey means the key is unknown or revoked.
var ErrInvalidKey = errors.New("invalid or revoked key")

// Key is what an ingest key grants.
type Key struct {
	ProjectID       int64
	RateLimitPerSec int
}

// Lookup finds an active key in the source of truth.
type Lookup interface {
	Find(ctx context.Context, publicKey string) (Key, error)
}

const (
	cachePrefix = "ingest:key:"
	// Revocations take effect within PositiveTTL even if the cache is not
	// purged (the query API purges it on rotation).
	PositiveTTL = 60 * time.Second
	NegativeTTL = 10 * time.Second
)

// Resolver caches key lookups in Redis. If Redis is down it falls back to
// the lookup directly rather than rejecting traffic.
type Resolver struct {
	Redis  redis.UniversalClient
	Source Lookup
}

// LooksValid rejects obviously malformed keys before touching any store.
func LooksValid(k string) bool {
	if len(k) < 16 || len(k) > 128 || !strings.HasPrefix(k, "pk_") {
		return false
	}
	for _, c := range k {
		if !(c == '_' || c == '-' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

func (r *Resolver) Resolve(ctx context.Context, publicKey string) (Key, error) {
	if !LooksValid(publicKey) {
		return Key{}, ErrInvalidKey
	}
	ck := cachePrefix + publicKey
	if r.Redis != nil {
		if v, err := r.Redis.Get(ctx, ck).Result(); err == nil {
			if v == "-" {
				return Key{}, ErrInvalidKey
			}
			if k, ok := decode(v); ok {
				return k, nil
			}
		}
	}
	k, err := r.Source.Find(ctx, publicKey)
	if r.Redis != nil {
		switch {
		case errors.Is(err, ErrInvalidKey):
			r.Redis.Set(ctx, ck, "-", NegativeTTL)
		case err == nil:
			r.Redis.Set(ctx, ck, encode(k), PositiveTTL)
		}
	}
	return k, err
}

// Purge drops a key from the cache, e.g. right after it was revoked.
func Purge(ctx context.Context, rdb redis.UniversalClient, publicKey string) error {
	return rdb.Del(ctx, cachePrefix+publicKey).Err()
}

func encode(k Key) string { return fmt.Sprintf("%d:%d", k.ProjectID, k.RateLimitPerSec) }

func decode(s string) (Key, bool) {
	p, l, ok := strings.Cut(s, ":")
	if !ok {
		return Key{}, false
	}
	pid, err1 := strconv.ParseInt(p, 10, 64)
	lim, err2 := strconv.Atoi(l)
	return Key{ProjectID: pid, RateLimitPerSec: lim}, err1 == nil && err2 == nil
}

// Limiter is a fixed one-second window counter per key in Redis.
type Limiter struct {
	Redis redis.UniversalClient
	Now   func() time.Time
}

// Allow counts n records against the key's per-second budget. It fails open
// when Redis is unavailable so an outage of the cache does not drop data.
func (l *Limiter) Allow(ctx context.Context, publicKey string, limit, n int) (bool, time.Duration) {
	if l.Redis == nil || limit <= 0 {
		return true, 0
	}
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	t := now()
	bucket := fmt.Sprintf("ingest:rl:%s:%d", publicKey, t.Unix())
	pipe := l.Redis.TxPipeline()
	incr := pipe.IncrBy(ctx, bucket, int64(n))
	pipe.Expire(ctx, bucket, 2*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return true, 0
	}
	if incr.Val() > int64(limit) {
		return false, t.Truncate(time.Second).Add(time.Second).Sub(t)
	}
	return true, 0
}

// Store reads and writes keys in Postgres. Redis, when set, is the gateway
// cache that Purge clears.
type Store struct {
	DB    *pgxpool.Pool
	Redis redis.UniversalClient
}

// Purge removes a key from the gateway cache.
func (s *Store) Purge(ctx context.Context, publicKey string) error {
	if s.Redis == nil {
		return nil
	}
	return Purge(ctx, s.Redis, publicKey)
}

func (s *Store) Find(ctx context.Context, publicKey string) (Key, error) {
	var k Key
	err := s.DB.QueryRow(ctx, `SELECT project_id, rate_limit_per_sec FROM api_keys
		WHERE public_key = $1 AND revoked_at IS NULL`, publicKey).Scan(&k.ProjectID, &k.RateLimitPerSec)
	if errors.Is(err, pgx.ErrNoRows) {
		return Key{}, ErrInvalidKey
	}
	return k, err
}

// APIKey is a key as shown in project settings.
type APIKey struct {
	ID              int64      `json:"id"`
	ProjectID       int64      `json:"project_id"`
	PublicKey       string     `json:"public_key"`
	RateLimitPerSec int        `json:"rate_limit_per_sec"`
	CreatedAt       time.Time  `json:"created_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

func (s *Store) List(ctx context.Context, projectID int64) ([]APIKey, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, project_id, public_key, rate_limit_per_sec, created_at, revoked_at
		FROM api_keys WHERE project_id = $1 ORDER BY revoked_at IS NULL DESC, created_at DESC LIMIT 20`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.ProjectID, &k.PublicKey, &k.RateLimitPerSec, &k.CreatedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// NewPublicKey returns "pk_" followed by 32 random hex characters.
func NewPublicKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "pk_" + hex.EncodeToString(b), nil
}

// Rotate issues a new key for the project and revokes all previous ones in
// one transaction. It returns the new key and the revoked public keys so the
// caller can purge them from the cache.
func (s *Store) Rotate(ctx context.Context, projectID int64) (APIKey, []string, error) {
	pk, err := NewPublicKey()
	if err != nil {
		return APIKey{}, nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return APIKey{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `UPDATE api_keys SET revoked_at = now()
		WHERE project_id = $1 AND revoked_at IS NULL RETURNING public_key`, projectID)
	if err != nil {
		return APIKey{}, nil, err
	}
	var revoked []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return APIKey{}, nil, err
		}
		revoked = append(revoked, k)
	}
	rows.Close()
	var k APIKey
	err = tx.QueryRow(ctx, `INSERT INTO api_keys (project_id, public_key) VALUES ($1, $2)
		RETURNING id, project_id, public_key, rate_limit_per_sec, created_at, revoked_at`, projectID, pk).
		Scan(&k.ID, &k.ProjectID, &k.PublicKey, &k.RateLimitPerSec, &k.CreatedAt, &k.RevokedAt)
	if err != nil {
		return APIKey{}, nil, err
	}
	return k, revoked, tx.Commit(ctx)
}

// Create adds a new active key without touching the existing ones.
func (s *Store) Create(ctx context.Context, projectID int64) (APIKey, error) {
	pk, err := NewPublicKey()
	if err != nil {
		return APIKey{}, err
	}
	var k APIKey
	err = s.DB.QueryRow(ctx, `INSERT INTO api_keys (project_id, public_key) VALUES ($1, $2)
		RETURNING id, project_id, public_key, rate_limit_per_sec, created_at, revoked_at`, projectID, pk).
		Scan(&k.ID, &k.ProjectID, &k.PublicKey, &k.RateLimitPerSec, &k.CreatedAt, &k.RevokedAt)
	return k, err
}

// ErrKeyNotFound means the key does not exist in the project.
var ErrKeyNotFound = errors.New("key not found")

// Revoke revokes one key of the project. Revoking an already revoked key
// returns it unchanged.
func (s *Store) Revoke(ctx context.Context, projectID, keyID int64) (APIKey, error) {
	var k APIKey
	err := s.DB.QueryRow(ctx, `UPDATE api_keys SET revoked_at = COALESCE(revoked_at, now())
		WHERE id = $1 AND project_id = $2
		RETURNING id, project_id, public_key, rate_limit_per_sec, created_at, revoked_at`, keyID, projectID).
		Scan(&k.ID, &k.ProjectID, &k.PublicKey, &k.RateLimitPerSec, &k.CreatedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrKeyNotFound
	}
	return k, err
}
