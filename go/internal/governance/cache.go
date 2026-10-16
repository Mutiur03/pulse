package governance

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"
)

// SettingsGetter loads a project's settings.
type SettingsGetter interface {
	Settings(ctx context.Context, project int64) (Settings, error)
}

// CacheTTL is how long the ingest gateway keeps a project's rules; saved
// changes apply within it.
const CacheTTL = 30 * time.Second

// Cache serves compiled scrubbers to the ingest gateway. If settings cannot
// be loaded it keeps the last rules it had, or uses the defaults, so an
// outage never turns scrubbing off.
type Cache struct {
	Source SettingsGetter
	TTL    time.Duration
	Now    func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	s       *Scrubber
	expires time.Time
}

var defaultScrubber, _ = Compile(DefaultRules())

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Scrubber returns the project's scrubber.
func (c *Cache) Scrubber(ctx context.Context, project string) *Scrubber {
	now := c.now()
	c.mu.Lock()
	e, ok := c.entries[project]
	c.mu.Unlock()
	if ok && now.Before(e.expires) {
		return e.s
	}
	ttl := c.TTL
	if ttl == 0 {
		ttl = CacheTTL
	}
	s := c.load(ctx, project)
	if s == nil {
		if ok {
			s = e.s
		} else {
			s = defaultScrubber
		}
		ttl = 5 * time.Second // retry soon
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]cacheEntry{}
	}
	c.entries[project] = cacheEntry{s: s, expires: now.Add(ttl)}
	c.mu.Unlock()
	return s
}

func (c *Cache) load(ctx context.Context, project string) *Scrubber {
	id, err := strconv.ParseInt(project, 10, 64)
	if err != nil {
		return defaultScrubber
	}
	st, err := c.Source.Settings(ctx, id)
	if err != nil {
		log.Printf("data settings for project %s: %v", project, err)
		return nil
	}
	s, err := Compile(st.Rules)
	if err != nil {
		log.Printf("data settings for project %s: %v", project, err)
		return nil
	}
	return s
}
