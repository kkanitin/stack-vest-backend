package cache

import (
	"sync"
	"time"
)

// TTL is a single-value in-process cache with a fixed time-to-live.
// Multiple concurrent misses may each call the fill function (acceptable stampede
// for low-traffic endpoints); the last writer wins.
type TTL[T any] struct {
	mu      sync.RWMutex
	value   T
	expires time.Time
	ttl     time.Duration
}

func NewTTL[T any](ttl time.Duration) *TTL[T] {
	return &TTL[T]{ttl: ttl}
}

// Get returns the cached value and true when the cache is still valid.
func (c *TTL[T]) Get() (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Now().Before(c.expires) {
		return c.value, true
	}
	var zero T
	return zero, false
}

// Set stores v and resets the expiry.
func (c *TTL[T]) Set(v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = v
	c.expires = time.Now().Add(c.ttl)
}
