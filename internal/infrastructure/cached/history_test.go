package cached

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

type countingFetcher struct {
	calls    atomic.Int32
	gate     chan struct{} // when non-nil, fetches block until closed
	points   []stockdomain.HistoryPoint
	err      error
	lastFrom time.Time
	lastTo   time.Time
	mu       sync.Mutex
}

func (f *countingFetcher) GetHistoryClose(_ string, from, to time.Time) ([]stockdomain.HistoryPoint, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.lastFrom, f.lastTo = from, to
	f.mu.Unlock()
	if f.gate != nil {
		<-f.gate
	}
	return f.points, f.err
}

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestHistoryCloser_ClipsToRequestedWindowFromOneFetch(t *testing.T) {
	inner := &countingFetcher{points: []stockdomain.HistoryPoint{
		{Date: "2026-09-01", Close: 1}, {Date: "2026-09-02", Close: 2}, {Date: "2026-09-03", Close: 3}, {Date: "2026-09-04", Close: 4},
	}}
	h := NewHistoryCloser(inner, time.Hour)

	tests := []struct {
		name     string
		from, to string
		want     []float64
	}{
		{"inclusive bounds", "2026-09-02", "2026-09-03", []float64{2, 3}},
		{"everything", "2020-01-01", "2030-01-01", []float64{1, 2, 3, 4}},
		{"none", "2027-01-01", "2027-02-01", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := h.GetHistoryClose("SPY", day(tc.from), day(tc.to))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
			for i, w := range tc.want {
				if got[i].Close != w {
					t.Fatalf("expected %v, got %v", tc.want, got)
				}
			}
		})
	}
	if n := inner.calls.Load(); n != 1 {
		t.Fatalf("expected one upstream fetch for all windows, got %d", n)
	}
}

func TestHistoryCloser_FetchesFiveYearWindow(t *testing.T) {
	inner := &countingFetcher{points: []stockdomain.HistoryPoint{{Date: "2026-09-01", Close: 1}}}
	h := NewHistoryCloser(inner, time.Hour)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }

	if _, err := h.GetHistoryClose("SPY", day("2026-09-01"), day("2026-09-30")); err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2021, 10, 9, 12, 0, 0, 0, time.UTC).Add(-14 * 24 * time.Hour)
	if !inner.lastFrom.Equal(wantFrom) || !inner.lastTo.Equal(now) {
		t.Fatalf("expected window %v..%v, got %v..%v", wantFrom, now, inner.lastFrom, inner.lastTo)
	}
}

func TestHistoryCloser_ConcurrentMissesCoalesce(t *testing.T) {
	inner := &countingFetcher{
		gate:   make(chan struct{}),
		points: []stockdomain.HistoryPoint{{Date: "2026-09-01", Close: 1}},
	}
	h := NewHistoryCloser(inner, time.Hour)

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.GetHistoryClose("SPY", day("2026-09-01"), day("2026-09-30")); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	// Wait until the first fetch is in flight, give the others time to pile up behind it.
	for inner.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(inner.gate)
	wg.Wait()

	if c := inner.calls.Load(); c != 1 {
		t.Fatalf("expected concurrent misses to share one fetch, got %d", c)
	}
}

func TestHistoryCloser_ErrorsAreNotCached(t *testing.T) {
	inner := &countingFetcher{err: errors.New("upstream down")}
	h := NewHistoryCloser(inner, time.Hour)

	for i := 0; i < 2; i++ {
		if _, err := h.GetHistoryClose("SPY", day("2026-09-01"), day("2026-09-30")); err == nil {
			t.Fatal("expected the upstream error")
		}
	}
	if c := inner.calls.Load(); c != 2 {
		t.Fatalf("expected a retry after an error, got %d fetches", c)
	}

	inner.err = nil
	inner.points = []stockdomain.HistoryPoint{{Date: "2026-09-01", Close: 1}}
	if got, err := h.GetHistoryClose("SPY", day("2026-09-01"), day("2026-09-30")); err != nil || len(got) != 1 {
		t.Fatalf("expected recovery, got %v %v", got, err)
	}
}
