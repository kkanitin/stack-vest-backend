package cache

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Keyed is an in-process TTL cache keyed by K, with per-key expiry and
// singleflight-coalesced fills. Use this where a single-value TTL cache (see
// TTL[T]) doesn't fit — per-symbol or per-query caching.
//
// No background eviction sweep runs (matches TTL[T]'s pragmatism — see its
// doc comment). When maxSize > 0, adding a key that would exceed it clears
// the whole map first; this is a deliberately crude bound for a cache keyed
// by caller-influenced input (e.g. search keywords) where the key space isn't
// naturally small. Pass maxSize 0 for keys with bounded cardinality (e.g.
// ticker symbols) where this never triggers in practice.
type Keyed[K comparable, V any] struct {
	mu      sync.RWMutex
	items   map[K]keyedEntry[V]
	ttl     time.Duration
	maxSize int
	sf      singleflight.Group
}

type keyedEntry[V any] struct {
	value   V
	expires time.Time
}

func NewKeyed[K comparable, V any](ttl time.Duration, maxSize int) *Keyed[K, V] {
	return &Keyed[K, V]{items: make(map[K]keyedEntry[V]), ttl: ttl, maxSize: maxSize}
}

// Get returns the cached value for key and true when it is still valid.
func (c *Keyed[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expires) {
		var zero V
		return zero, false
	}
	return e.value, true
}

// Set stores v for key and resets its expiry.
func (c *Keyed[K, V]) Set(key K, v V) {
	c.mu.Lock()
	if c.maxSize > 0 && len(c.items) >= c.maxSize {
		c.items = make(map[K]keyedEntry[V])
	}
	c.items[key] = keyedEntry[V]{value: v, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// Fill returns the cached value for key, calling fn on a miss. Concurrent
// misses for the same key are coalesced via singleflight.
func (c *Keyed[K, V]) Fill(key K, fn func() (V, error)) (V, error) {
	if v, ok := c.Get(key); ok {
		return v, nil
	}
	v, err, _ := c.sf.Do(fmt.Sprint(key), func() (any, error) {
		if v, ok := c.Get(key); ok {
			return v, nil
		}
		val, err := fn()
		if err != nil {
			return val, err
		}
		c.Set(key, val)
		return val, nil
	})
	return v.(V), err
}
