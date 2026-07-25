package cache

import (
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// TTL is a single-value in-process cache with a fixed time-to-live. Concurrent
// misses are coalesced via singleflight when filled through Fill, so a fill
// function runs at most once per expiry window regardless of how many
// goroutines call Fill while cold. Callers using bare Get/Set instead of Fill
// keep the older "last writer wins" behavior (acceptable stampede for
// low-traffic endpoints).
type TTL[T any] struct {
	mu          sync.RWMutex
	value       T
	expires     time.Time
	negative    bool // whether the live value was stored via SetNegative
	ttl         time.Duration
	negativeTTL time.Duration
	hits        atomic.Int64
	misses      atomic.Int64
	sf          singleflight.Group
}

func NewTTL[T any](ttl time.Duration) *TTL[T] {
	return &TTL[T]{ttl: ttl, negativeTTL: ttl}
}

// NewTTLWithNegative sets a shorter TTL for values stored via SetNegative (or
// via Fill when its fn reports healthy=false), so a failed/degraded fill is
// retried sooner than a healthy one.
func NewTTLWithNegative[T any](ttl, negativeTTL time.Duration) *TTL[T] {
	return &TTL[T]{ttl: ttl, negativeTTL: negativeTTL}
}

// Get returns the cached value and true when the cache is still valid,
// regardless of whether it was stored via Set or SetNegative. Callers that
// need to distinguish a healthy value from a cached negative result should
// use Fill instead.
func (c *TTL[T]) Get() (T, bool) {
	v, valid, _ := c.get()
	return v, valid
}

func (c *TTL[T]) get() (v T, valid, healthy bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Now().Before(c.expires) {
		c.hits.Add(1)
		return c.value, true, !c.negative
	}
	c.misses.Add(1)
	var zero T
	return zero, false, false
}

// Set stores v and resets the expiry to the base TTL.
func (c *TTL[T]) Set(v T) {
	c.set(v, c.ttl, false)
}

// SetNegative stores v under the shorter negative TTL.
func (c *TTL[T]) SetNegative(v T) {
	c.set(v, c.negativeTTL, true)
}

func (c *TTL[T]) set(v T, ttl time.Duration, negative bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = v
	c.expires = time.Now().Add(ttl)
	c.negative = negative
}

// Stats returns cumulative hit/miss counts since construction.
func (c *TTL[T]) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

type fillResult[T any] struct {
	v       T
	healthy bool
}

// Fill returns the cached value and whether it's healthy, calling fn on a
// miss. Concurrent misses are coalesced via singleflight — fn runs once, and
// its (value, healthy) result is shared with every waiting caller, not just
// the one that executed fn.
//
// When fn reports healthy=false, v is cached under the shorter negativeTTL
// instead of the base ttl, and healthy=false is returned — including on a
// later warm hit within that negativeTTL window, so callers get a consistent
// answer whether the result just came from fn or from the negative cache.
func (c *TTL[T]) Fill(fn func() (v T, healthy bool)) (T, bool) {
	if v, valid, healthy := c.get(); valid {
		return v, healthy
	}
	res, _, _ := c.sf.Do("", func() (any, error) {
		if v, valid, healthy := c.get(); valid {
			return fillResult[T]{v, healthy}, nil
		}
		v, healthy := fn()
		if healthy {
			c.Set(v)
		} else {
			c.SetNegative(v)
		}
		return fillResult[T]{v, healthy}, nil
	})
	r := res.(fillResult[T])
	return r.v, r.healthy
}
