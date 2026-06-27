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

type mockUserFinder struct{ id string }

func (m *mockUserFinder) FindByEmail(context.Context, string) (*userdomain.User, error) {
	return &userdomain.User{ID: m.id}, nil
}

type mockPositionLister struct{ positions []*portfoliodomain.Position }

func (m *mockPositionLister) ListPositionsByUser(context.Context, string) ([]*portfoliodomain.Position, error) {
	return m.positions, nil
}

type mockFetcher struct {
	events []dividenddomain.Event
	err    error
	calls  int
}

func (m *mockFetcher) GetDividendsCalendar(time.Time, time.Time) ([]dividenddomain.Event, error) {
	m.calls++
	return m.events, m.err
}

// mockCache is a single-slot dividenddomain.Cache (there is one calendar window per
// Execute, so the key is irrelevant to the test).
type mockCache struct {
	events   []dividenddomain.Event
	present  bool
	setCalls int
}

func (c *mockCache) Get(context.Context, string) ([]dividenddomain.Event, bool, error) {
	return c.events, c.present, nil
}

func (c *mockCache) Set(_ context.Context, _ string, events []dividenddomain.Event) error {
	c.events = events
	c.present = true
	c.setCalls++
	return nil
}

// inDays returns a UTC date offset from today, matching the use case's day-truncated
// window so test events land inside the fetched range.
func inDays(d int) time.Time {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, d)
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

func TestCalendar_CacheMissFetchesAndStores(t *testing.T) {
	cache := &mockCache{}
	fetcher := &mockFetcher{events: []dividenddomain.Event{
		{Symbol: "KO", PaymentDate: inDays(20), Dividend: 0.5},
	}}

	uc := dividenduc.NewCalendarUseCase(
		&mockUserFinder{id: "u1"},
		&mockPositionLister{positions: []*portfoliodomain.Position{{Symbol: "KO", Shares: 4}}},
		fetcher, cache,
	)

	entries, err := uc.Execute(context.Background(), "a@b.com", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if fetcher.calls != 1 {
		t.Errorf("expected 1 fetch on miss, got %d", fetcher.calls)
	}
	if cache.setCalls != 1 {
		t.Errorf("expected cache Set once, got %d", cache.setCalls)
	}
	if got := entries[0].EstimatedAmount; got != 2.0 {
		t.Errorf("estimated amount: want 2.0, got %v", got)
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
