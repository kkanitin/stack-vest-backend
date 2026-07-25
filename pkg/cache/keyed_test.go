package cache

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyed_GetSet(t *testing.T) {
	c := NewKeyed[string, int](time.Minute, 0)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected miss on empty cache")
	}
	c.Set("a", 1)
	c.Set("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("expected (1, true) for key a, got (%v, %v)", v, ok)
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("expected (2, true) for key b, got (%v, %v)", v, ok)
	}
}

func TestKeyed_Expiry(t *testing.T) {
	c := NewKeyed[string, int](10*time.Millisecond, 0)
	c.Set("a", 1)
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestKeyed_Fill_CachesPerKey(t *testing.T) {
	c := NewKeyed[string, int](time.Minute, 0)
	var calls atomic.Int32
	fill := func(v int) func() (int, error) {
		return func() (int, error) {
			calls.Add(1)
			return v, nil
		}
	}

	v, err := c.Fill("a", fill(1))
	if err != nil || v != 1 {
		t.Fatalf("expected (1, nil), got (%v, %v)", v, err)
	}
	v, err = c.Fill("b", fill(2))
	if err != nil || v != 2 {
		t.Fatalf("expected (2, nil), got (%v, %v)", v, err)
	}
	// Repeat fetch of "a" must not call fn again.
	v, err = c.Fill("a", fill(999))
	if err != nil || v != 1 {
		t.Fatalf("expected cached (1, nil) for key a, got (%v, %v)", v, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected fn called once per distinct key (2 total), got %d", got)
	}
}

func TestKeyed_Fill_PropagatesError(t *testing.T) {
	c := NewKeyed[string, int](time.Minute, 0)
	wantErr := errors.New("upstream failed")
	_, err := c.Fill("a", func() (int, error) { return 0, wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error to propagate, got %v", err)
	}
	// An errored fill isn't cached — a retry should call fn again.
	var calls atomic.Int32
	_, _ = c.Fill("a", func() (int, error) {
		calls.Add(1)
		return 5, nil
	})
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected fn to be retried after a prior error, got %d calls", got)
	}
}

func TestKeyed_Fill_CoalescesConcurrentMissesPerKey(t *testing.T) {
	c := NewKeyed[string, int](time.Minute, 0)
	var calls atomic.Int32
	release := make(chan struct{})
	fn := func() (int, error) {
		calls.Add(1)
		<-release
		return 42, nil
	}

	const n = 20
	var wg sync.WaitGroup
	results := make([]int, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			v, err := c.Fill("shared-key", fn)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			results[i] = v
		}(i)
	}

	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected fn called exactly once across %d concurrent Fill calls on the same key, got %d", n, got)
	}
	for i, v := range results {
		if v != 42 {
			t.Fatalf("caller %d: expected 42, got %v", i, v)
		}
	}
}

func TestKeyed_MaxSizeClearsOnOverflow(t *testing.T) {
	c := NewKeyed[string, int](time.Minute, 2)
	c.Set("a", 1)
	c.Set("b", 2)
	// Adding a third key at maxSize=2 clears the map first (crude bound —
	// see Keyed's doc comment), so "a" and "b" no longer hit.
	c.Set("c", 3)

	if _, ok := c.Get("a"); ok {
		t.Fatal("expected key 'a' to be evicted by the maxSize clear")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("expected key 'b' to be evicted by the maxSize clear")
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("expected key 'c' to survive its own Set, got (%v, %v)", v, ok)
	}
}
