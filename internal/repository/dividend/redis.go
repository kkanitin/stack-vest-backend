package dividend

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"

	dividenddomain "github.com/kanitin/stackvest/backend/internal/domain/dividend"
)

// keyPrefix is versioned so the cached encoding can be changed without colliding
// with stale values from a previous shape.
const keyPrefix = "dividend:v1:"

// RedisCache is a shared dividend-calendar cache backed by Redis. It implements
// dividenddomain.Cache. Because the dividend calendar is market-wide reference
// data, a single cached blob (keyed by the fetch window) serves every user.
//
// Two stampede mitigations live here:
//   - TTL jitter: each key's expiry is base ± jitterPct, so keys populated close
//     together don't all expire at the same instant (cache avalanche).
//   - Negative caching: an empty result is cached under a shorter TTL so an empty
//     window doesn't re-hit the provider on every request.
//
// (Single-flight coalescing of concurrent misses on one hot key lives in the use
// case, since it must wrap the provider call, not the cache.)
type RedisCache struct {
	client      *redis.Client
	ttl         time.Duration // base TTL for a non-empty calendar window
	negativeTTL time.Duration // TTL for the empty-result case
	jitterPct   float64       // fraction of ttl to randomise expiry by, e.g. 0.10
}

// NewRedisCache builds a cache with a base TTL (e.g. 24h) and a shorter negative
// TTL (e.g. 1h) for empty results. Jitter is fixed at ±10%.
func NewRedisCache(client *redis.Client, ttl, negativeTTL time.Duration) *RedisCache {
	return &RedisCache{
		client:      client,
		ttl:         ttl,
		negativeTTL: negativeTTL,
		jitterPct:   0.10,
	}
}

func (c *RedisCache) Get(ctx context.Context, key string) ([]dividenddomain.Event, bool, error) {
	data, err := c.client.Get(ctx, keyPrefix+key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("redis get %s: %w", key, err)
	}

	// A non-nil, possibly empty slice is a valid hit (negative cache).
	events := []dividenddomain.Event{}
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, false, fmt.Errorf("dividend cache decode %s: %w", key, err)
	}
	return events, true, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, events []dividenddomain.Event) error {
	data, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("dividend cache encode %s: %w", key, err)
	}

	base := c.ttl
	if len(events) == 0 {
		base = c.negativeTTL
	}
	if err := c.client.Set(ctx, keyPrefix+key, data, jitter(base, c.jitterPct)).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key, err)
	}
	return nil
}

// jitter returns base scaled by a random factor in [1-pct, 1+pct].
func jitter(base time.Duration, pct float64) time.Duration {
	if pct <= 0 {
		return base
	}
	factor := 1 + pct*(2*rand.Float64()-1)
	return time.Duration(float64(base) * factor)
}
