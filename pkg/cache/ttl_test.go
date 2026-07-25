package cache

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTTL_GetSet(t *testing.T) {
	c := NewTTL[int](time.Minute)
	if _, ok := c.Get(); ok {
		t.Fatal("expected miss on empty cache")
	}
	c.Set(42)
	v, ok := c.Get()
	if !ok || v != 42 {
		t.Fatalf("expected hit with value 42, got %v, %v", v, ok)
	}
}

func TestTTL_Expiry(t *testing.T) {
	c := NewTTL[int](10 * time.Millisecond)
	c.Set(1)
	if _, ok := c.Get(); !ok {
		t.Fatal("expected hit immediately after Set")
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get(); ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestTTL_Fill_CachesHealthyResult(t *testing.T) {
	c := NewTTL[int](time.Minute)
	var calls atomic.Int32
	fn := func() (int, bool) {
		calls.Add(1)
		return 7, true
	}

	v, healthy := c.Fill(fn)
	if !healthy || v != 7 {
		t.Fatalf("expected (7, true), got (%v, %v)", v, healthy)
	}
	// Second call should be served from cache, not call fn again.
	v, healthy = c.Fill(fn)
	if !healthy || v != 7 {
		t.Fatalf("expected cached (7, true), got (%v, %v)", v, healthy)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected fn called once, got %d", got)
	}
}

func TestTTL_Fill_NegativeCacheShorterTTL(t *testing.T) {
	c := NewTTLWithNegative[int](time.Hour, 10*time.Millisecond)
	var calls atomic.Int32
	fn := func() (int, bool) {
		calls.Add(1)
		return 0, false
	}

	v, healthy := c.Fill(fn)
	if healthy || v != 0 {
		t.Fatalf("expected (0, false) on failed fill, got (%v, %v)", v, healthy)
	}

	// Warm hit within the negative TTL window: still unhealthy, fn not re-called.
	v, healthy = c.Fill(fn)
	if healthy {
		t.Fatal("expected warm negative-cache hit to report healthy=false")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected fn called once before negative TTL expiry, got %d", got)
	}

	// After the negative TTL expires, fn runs again.
	time.Sleep(20 * time.Millisecond)
	if _, healthy = c.Fill(fn); healthy {
		t.Fatal("expected fn to still report unhealthy on retry")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected fn called again after negative TTL expiry, got %d", got)
	}
}

func TestTTL_Fill_CoalescesConcurrentMisses(t *testing.T) {
	c := NewTTL[int](time.Minute)
	var calls atomic.Int32
	release := make(chan struct{})
	fn := func() (int, bool) {
		calls.Add(1)
		<-release // block until every goroutine has had a chance to call Fill
		return 99, true
	}

	const n = 20
	var wg sync.WaitGroup
	results := make([]int, n)
	healths := make([]bool, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], healths[i] = c.Fill(fn)
		}(i)
	}

	time.Sleep(20 * time.Millisecond) // let every goroutine reach Fill and block in fn
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected fn called exactly once across %d concurrent Fill calls, got %d", n, got)
	}
	for i, v := range results {
		if v != 99 || !healths[i] {
			t.Fatalf("caller %d: expected (99, true), got (%v, %v)", i, v, healths[i])
		}
	}
}

func TestTTL_Stats(t *testing.T) {
	c := NewTTL[int](time.Minute)
	c.Get() // miss
	c.Set(1)
	c.Get() // hit
	c.Get() // hit
	hits, misses := c.Stats()
	if hits != 2 || misses != 1 {
		t.Fatalf("expected 2 hits / 1 miss, got %d hits / %d misses", hits, misses)
	}
}
