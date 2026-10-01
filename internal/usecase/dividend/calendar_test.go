package dividend_test

import (
	"context"
	"errors"
	"testing"
	"time"

	dividenddomain "github.com/kanitin/stackvest/backend/internal/domain/dividend"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	dividenduc "github.com/kanitin/stackvest/backend/internal/usecase/dividend"
)

type mockUserFinder struct {
	id    string
	calls int
}

func (m *mockUserFinder) FindByEmail(context.Context, string) (*userdomain.User, error) {
	m.calls++
	return &userdomain.User{ID: m.id}, nil
}

type mockPositionLister struct{ positions []*portfoliodomain.Position }

func (m *mockPositionLister) ListPositionsByUser(context.Context, string) ([]*portfoliodomain.Position, error) {
	return m.positions, nil
}

// mockFetcher returns the same events for every range and records the ranges asked for.
type mockFetcher struct {
	events  []dividenddomain.Event
	err     error
	calls   int
	froms   []time.Time
	tos     []time.Time
	onFetch func() // runs during each fetch, e.g. to cancel the request mid-fill
}

func (m *mockFetcher) GetDividendsCalendar(from, to time.Time) ([]dividenddomain.Event, error) {
	m.calls++
	m.froms = append(m.froms, from)
	m.tos = append(m.tos, to)
	if m.onFetch != nil {
		m.onFetch()
	}
	return m.events, m.err
}

// mockCache is a dividenddomain.Cache with two layers: store holds what Set wrote,
// per key, and events/present is a seeded slot that answers every key Set has not
// written. The seeded slot stands in for "every month bucket is already cached", so
// a test can seed one slice without knowing which months its window covers (each
// bucket then returns the whole slice and the use case's per-bucket date filter
// picks the right events).
//
// honorCtx makes Get and Set fail on a cancelled context, as a real Redis client does.
type mockCache struct {
	store    map[string][]dividenddomain.Event
	events   []dividenddomain.Event
	present  bool
	setCalls int
	honorCtx bool
}

func (c *mockCache) Get(ctx context.Context, key string) ([]dividenddomain.Event, bool, error) {
	if c.honorCtx && ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if events, ok := c.store[key]; ok {
		return events, true, nil
	}
	return c.events, c.present, nil
}

func (c *mockCache) Set(ctx context.Context, key string, events []dividenddomain.Event) error {
	if c.honorCtx && ctx.Err() != nil {
		return ctx.Err()
	}
	if c.store == nil {
		c.store = make(map[string][]dividenddomain.Event)
	}
	c.store[key] = events
	c.setCalls++
	return nil
}

// inDays returns a UTC date offset from today, matching the use case's day-truncated
// window so test events land inside the requested range.
func inDays(d int) time.Time {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, d)
}

// monthBounds returns the first and last day of t's calendar month.
func monthBounds(t time.Time) (time.Time, time.Time) {
	start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, -1)
}

func TestCalendar_CacheHitSkipsFetcher(t *testing.T) {
	cache := &mockCache{
		present: true,
		events:  []dividenddomain.Event{{Symbol: "AAPL", PaymentDate: inDays(10), Dividend: 0.25}},
	}
	fetcher := &mockFetcher{}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "AAPL", Shares: 10}}},
		fetcher, cache,
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if fetcher.calls != 0 {
		t.Errorf("expected fetcher not called on cache hit, got %d", fetcher.calls)
	}
	if got := entries[0].EstimatedAmount; got != 2.5 {
		t.Errorf("estimated amount: want 2.5, got %v", got)
	}
}

// TestCalendar_CacheMissFetchesAndStores uses a one-day window so exactly one month
// bucket is involved whatever today's date is: the miss fills that bucket with one
// fetch of [month start − 45d, month end], and a second Execute is served from cache.
func TestCalendar_CacheMissFetchesAndStores(t *testing.T) {
	theDay := inDays(20)
	cache := &mockCache{}
	fetcher := &mockFetcher{events: []dividenddomain.Event{
		{Symbol: "KO", PaymentDate: theDay, Dividend: 0.5},
	}}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "KO", Shares: 4}}},
		fetcher, cache,
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", theDay, theDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if fetcher.calls != 1 {
		t.Fatalf("expected 1 fetch on miss, got %d", fetcher.calls)
	}
	if cache.setCalls != 1 {
		t.Errorf("expected cache Set once, got %d", cache.setCalls)
	}
	if got := entries[0].EstimatedAmount; got != 2.0 {
		t.Errorf("estimated amount: want 2.0, got %v", got)
	}

	monthStart, monthEnd := monthBounds(theDay)
	if wantFrom := monthStart.AddDate(0, 0, -45); !fetcher.froms[0].Equal(wantFrom) {
		t.Errorf("fetch from: want %s (month start − 45d), got %s", wantFrom, fetcher.froms[0])
	}
	if !fetcher.tos[0].Equal(monthEnd) {
		t.Errorf("fetch to: want %s (month end), got %s", monthEnd, fetcher.tos[0])
	}

	again, err := uc.Execute(context.Background(), "a@b.com", theDay, theDay)
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if len(again) != 1 {
		t.Errorf("expected 1 entry from the cached bucket, got %d", len(again))
	}
	if fetcher.calls != 1 {
		t.Errorf("expected no further fetch once the bucket is cached, got %d", fetcher.calls)
	}
}

// TestCalendar_BucketStoresOnlyItsMonth pins what a fill caches: the 45-day lookback
// brings in events that belong to earlier months, and they must not be stored in
// this month's bucket.
func TestCalendar_BucketStoresOnlyItsMonth(t *testing.T) {
	theDay := inDays(20)
	monthStart, _ := monthBounds(theDay)
	cache := &mockCache{}
	fetcher := &mockFetcher{events: []dividenddomain.Event{
		{Symbol: "KO", PaymentDate: theDay, Dividend: 0.5},
		{Symbol: "KO", PaymentDate: monthStart.AddDate(0, 0, -1), Dividend: 0.5}, // previous month
	}}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "KO", Shares: 4}}},
		fetcher, cache,
	)

	if _, err := uc.Execute(context.Background(), "a@b.com", theDay, theDay); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stored, ok := cache.store["calendar:"+theDay.Format("2006-01")]
	if !ok {
		t.Fatalf("expected the month bucket to be cached, have keys %v", cache.store)
	}
	if len(stored) != 1 || !stored[0].PaymentDate.Equal(theDay) {
		t.Errorf("expected only the in-month event in the bucket, got %+v", stored)
	}
}

// TestCalendar_EmptyCachedBucketIsAHit pins negative caching: a cached month with no
// events must not be re-fetched.
func TestCalendar_EmptyCachedBucketIsAHit(t *testing.T) {
	fetcher := &mockFetcher{}
	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "KO", Shares: 4}}},
		fetcher, &mockCache{present: true, events: []dividenddomain.Event{}},
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %d", len(entries))
	}
	if fetcher.calls != 0 {
		t.Errorf("expected an empty cached bucket to be a hit, got %d fetches", fetcher.calls)
	}
}

// TestCalendar_AbandonedRequestStillCachesTheFill cancels the request while the
// provider fetch is in flight. The fetch cannot be cancelled, so its result must be
// cached for the next caller rather than thrown away.
func TestCalendar_AbandonedRequestStillCachesTheFill(t *testing.T) {
	theDay := inDays(20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := &mockCache{honorCtx: true}
	fetcher := &mockFetcher{
		events:  []dividenddomain.Event{{Symbol: "KO", PaymentDate: theDay, Dividend: 0.5}},
		onFetch: cancel,
	}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "KO", Shares: 4}}},
		fetcher, cache,
	)

	uc.Execute(ctx, "a@b.com", theDay, theDay)

	if _, ok := cache.store["calendar:"+theDay.Format("2006-01")]; !ok {
		t.Errorf("expected the fill to be cached despite the cancelled request")
	}
}

// TestCalendar_CancelledRequestStopsBeforeTheNextMonth pins that an abandoned request
// does not go on filling the remaining months of its window.
func TestCalendar_CancelledRequestStopsBeforeTheNextMonth(t *testing.T) {
	_, lastDay := monthBounds(inDays(0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fetcher := &mockFetcher{onFetch: cancel}
	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "KO", Shares: 4}}},
		fetcher, &mockCache{honorCtx: true},
	)

	// Two months: the request is cancelled during the first month's fill.
	_, err := uc.Execute(ctx, "a@b.com", lastDay, lastDay.AddDate(0, 0, 1))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if fetcher.calls != 1 {
		t.Errorf("expected the second month not to be fetched, got %d fetches", fetcher.calls)
	}
}

// TestCalendar_PastWindowHonored pins that a window entirely in the past is served
// as asked rather than floored at today.
func TestCalendar_PastWindowHonored(t *testing.T) {
	fetcher := &mockFetcher{events: []dividenddomain.Event{
		{Symbol: "KO", PaymentDate: inDays(-20), Dividend: 0.5},
	}}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "KO", Shares: 4}}},
		fetcher, &mockCache{},
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", inDays(-40), inDays(-10))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 past entry, got %d", len(entries))
	}
	if !entries[0].PaymentDate.Equal(inDays(-20)) {
		t.Errorf("wrong entry kept: %v", entries[0].PaymentDate)
	}
}

// TestCalendar_MultiBucketNoDuplicates spans a month boundary with a two-day window.
// The seeded cache slot answers both month buckets with both events, so only the
// per-bucket date clamp keeps each event from being emitted once per bucket.
func TestCalendar_MultiBucketNoDuplicates(t *testing.T) {
	_, lastDay := monthBounds(inDays(0))
	firstDay := lastDay.AddDate(0, 0, 1)

	cache := &mockCache{
		present: true,
		events: []dividenddomain.Event{
			{Symbol: "AAPL", PaymentDate: lastDay, Dividend: 0.25},
			{Symbol: "AAPL", PaymentDate: firstDay, Dividend: 0.25},
		},
	}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "AAPL", Shares: 1}}},
		&mockFetcher{}, cache,
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", lastDay, firstDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (one per day, no duplicates), got %d", len(entries))
	}
	if !entries[0].PaymentDate.Equal(lastDay) || !entries[1].PaymentDate.Equal(firstDay) {
		t.Errorf("wrong entries: %v, %v", entries[0].PaymentDate, entries[1].PaymentDate)
	}
}

// TestCalendar_RangeErrors checks each range sentinel is returned before any I/O:
// no user lookup and no provider fetch.
func TestCalendar_RangeErrors(t *testing.T) {
	tests := []struct {
		name     string
		from, to time.Time
		wantErr  error
	}{
		{"to before from", inDays(10), inDays(5), dividenddomain.ErrInvalidRange},
		{"only to, before today", time.Time{}, inDays(-5), dividenddomain.ErrInvalidRange},
		{"span too long", inDays(0), inDays(93), dividenddomain.ErrRangeTooLong},
		{"too far in the past", inDays(-500), inDays(-490), dividenddomain.ErrRangeOutOfBounds},
		{"too far in the future", inDays(490), inDays(500), dividenddomain.ErrRangeOutOfBounds},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			users := &mockUserFinder{id: "u1"}
			fetcher := &mockFetcher{}
			uc := dividenduc.NewCalendarUseCase(
				users,
				&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "AAPL", Shares: 1}}},
				fetcher, &mockCache{},
			)

			if _, err := uc.Execute(context.Background(), "a@b.com", tc.from, tc.to); !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
			if users.calls != 0 {
				t.Errorf("expected no user lookup for an invalid range, got %d", users.calls)
			}
			if fetcher.calls != 0 {
				t.Errorf("expected no fetch for an invalid range, got %d", fetcher.calls)
			}
		})
	}
}

func TestCalendar_WindowFiltersOutOfRange(t *testing.T) {
	cache := &mockCache{
		present: true,
		events: []dividenddomain.Event{
			{Symbol: "AAPL", PaymentDate: inDays(10), Dividend: 0.25}, // in range
			{Symbol: "AAPL", PaymentDate: inDays(60), Dividend: 0.25}, // after to
		},
	}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "AAPL", Shares: 1}}},
		&mockFetcher{}, cache,
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", inDays(0), inDays(30))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 in-window entry, got %d", len(entries))
	}
	if !entries[0].PaymentDate.Equal(inDays(10)) {
		t.Errorf("wrong entry kept: %v", entries[0].PaymentDate)
	}
}

func TestCalendar_FiltersToHeldSymbolsAggregatesAndSorts(t *testing.T) {
	cache := &mockCache{
		present: true,
		events: []dividenddomain.Event{
			{Symbol: "AAPL", PaymentDate: inDays(40), Dividend: 0.25},
			{Symbol: "KO", PaymentDate: inDays(20), Dividend: 0.5},
			{Symbol: "MSFT", PaymentDate: inDays(15), Dividend: 0.75}, // not held → excluded
		},
	}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{
			{Symbol: "AAPL", Shares: 10, PortfolioID: "p1"},
			{Symbol: "AAPL", Shares: 5, PortfolioID: "p2"}, // same symbol, second portfolio
			{Symbol: "KO", Shares: 4, PortfolioID: "p1"},
		}},
		&mockFetcher{}, cache,
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (MSFT excluded), got %d", len(entries))
	}
	// Sorted by reference (payment) date ascending: KO (+20) before AAPL (+40).
	if entries[0].Symbol != "KO" || entries[1].Symbol != "AAPL" {
		t.Errorf("wrong sort order: %s, %s", entries[0].Symbol, entries[1].Symbol)
	}
	// AAPL shares aggregated across portfolios: (10+5) * 0.25 = 3.75.
	if got := entries[1].EstimatedAmount; got != 3.75 {
		t.Errorf("aggregated estimated amount: want 3.75, got %v", got)
	}
}

func TestCalendar_FetcherErrorPropagates(t *testing.T) {
	fetcher := &mockFetcher{err: errors.New("upstream down")}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "AAPL", Shares: 1}}},
		fetcher, &mockCache{},
	)

	if _, err := uc.Execute(context.Background(), "a@b.com", time.Time{}, time.Time{}); err == nil {
		t.Fatal("expected error when the only data source fails, got nil")
	}
}

func TestCalendar_NoHoldingsReturnsEmptyWithoutFetch(t *testing.T) {
	fetcher := &mockFetcher{}
	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: nil},
		fetcher, &mockCache{},
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %d", len(entries))
	}
	if fetcher.calls != 0 {
		t.Errorf("expected no fetch when user holds nothing, got %d", fetcher.calls)
	}
}
