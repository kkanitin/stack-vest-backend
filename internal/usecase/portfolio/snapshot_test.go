package portfolio_test

import (
	"context"
	"errors"
	"testing"
	"time"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
)

var snapshotDay = time.Date(2026, 10, 1, 15, 30, 0, 0, time.UTC)

func TestSnapshotValues_TotalsEachUserAcrossPortfolios(t *testing.T) {
	// u1 holds AAPL in two portfolios (2 + 3 shares) and MSFT; u2 holds MSFT only.
	repo := &mockRepo{listAllHoldings: func() ([]*portfoliodomain.UserHolding, error) {
		return []*portfoliodomain.UserHolding{
			{UserID: "u1", Symbol: "AAPL", Shares: 2},
			{UserID: "u1", Symbol: "AAPL", Shares: 3},
			{UserID: "u1", Symbol: "MSFT", Shares: 1},
			{UserID: "u2", Symbol: "MSFT", Shares: 4},
		}, nil
	}}
	q := stubQuoter{price: map[string]float64{"AAPL": 100, "MSFT": 50}}
	uc := newUCWithPrices(repo, &mockUserRepo{}, q, stubPriceChanger{})

	written, skipped, err := uc.SnapshotValues(context.Background(), snapshotDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != 2 || skipped != 0 {
		t.Fatalf("expected 2 written / 0 skipped, got %d / %d", written, skipped)
	}
	if repo.upserted["u1"] != 550 || repo.upserted["u2"] != 200 {
		t.Fatalf("expected u1=550, u2=200, got %v", repo.upserted)
	}
	if !repo.upsertDate.Equal(snapshotDay) {
		t.Fatalf("expected snapshot recorded for %v, got %v", snapshotDay, repo.upsertDate)
	}
}

func TestSnapshotValues_SkipsUserWithAnUnpricedHolding(t *testing.T) {
	// MSFT has no quote. A partial total for u1 would chart as a false dip, so u1 gets
	// no row this run; u2, fully priced, is still recorded.
	repo := &mockRepo{listAllHoldings: func() ([]*portfoliodomain.UserHolding, error) {
		return []*portfoliodomain.UserHolding{
			{UserID: "u1", Symbol: "AAPL", Shares: 2},
			{UserID: "u1", Symbol: "MSFT", Shares: 1},
			{UserID: "u2", Symbol: "AAPL", Shares: 1},
		}, nil
	}}
	q := selectiveQuoter{price: map[string]float64{"AAPL": 100}}
	uc := newUCWithPrices(repo, &mockUserRepo{}, q, stubPriceChanger{})

	written, skipped, err := uc.SnapshotValues(context.Background(), snapshotDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != 1 || skipped != 1 {
		t.Fatalf("expected 1 written / 1 skipped, got %d / %d", written, skipped)
	}
	if _, ok := repo.upserted["u1"]; ok {
		t.Fatalf("u1 has an unpriced holding and must not be recorded, got %v", repo.upserted)
	}
	if repo.upserted["u2"] != 100 {
		t.Fatalf("expected u2=100, got %v", repo.upserted)
	}
}

func TestSnapshotValues_ZeroPriceCountsAsUnpriced(t *testing.T) {
	// stubQuoter answers an unknown symbol with price 0 and no error; that must not be
	// recorded as a real $0 holding.
	repo := &mockRepo{listAllHoldings: func() ([]*portfoliodomain.UserHolding, error) {
		return []*portfoliodomain.UserHolding{{UserID: "u1", Symbol: "GHOST", Shares: 5}}, nil
	}}
	uc := newUCWithPrices(repo, &mockUserRepo{}, stubQuoter{price: map[string]float64{}}, stubPriceChanger{})

	written, skipped, err := uc.SnapshotValues(context.Background(), snapshotDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != 0 || skipped != 1 || len(repo.upserted) != 0 {
		t.Fatalf("expected nothing written, got written=%d skipped=%d upserted=%v", written, skipped, repo.upserted)
	}
}

func TestSnapshotValues_DoesNotNeedThePriceChange(t *testing.T) {
	// The snapshot is quote-only: a 30-day price-change outage must not cost a day of history.
	repo := &mockRepo{listAllHoldings: func() ([]*portfoliodomain.UserHolding, error) {
		return []*portfoliodomain.UserHolding{{UserID: "u1", Symbol: "AAPL", Shares: 2}}, nil
	}}
	q := stubQuoter{price: map[string]float64{"AAPL": 100}}
	uc := newUCWithPrices(repo, &mockUserRepo{}, q, failingPriceChanger{})

	written, _, err := uc.SnapshotValues(context.Background(), snapshotDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != 1 || repo.upserted["u1"] != 200 {
		t.Fatalf("expected u1=200 recorded despite the price-change outage, got written=%d %v", written, repo.upserted)
	}
}

func TestSnapshotValues_NoHoldingsWritesNothing(t *testing.T) {
	repo := &mockRepo{}
	uc := newUCWithPrices(repo, &mockUserRepo{}, failQuoter{}, stubPriceChanger{})

	written, skipped, err := uc.SnapshotValues(context.Background(), snapshotDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if written != 0 || skipped != 0 || repo.upsertCalls != 0 {
		t.Fatalf("expected no write, got written=%d skipped=%d upsertCalls=%d", written, skipped, repo.upsertCalls)
	}
}

func TestSnapshotValues_RepositoryErrorIsReturned(t *testing.T) {
	boom := errors.New("db down")
	repo := &mockRepo{listAllHoldings: func() ([]*portfoliodomain.UserHolding, error) { return nil, boom }}
	uc := newUCWithPrices(repo, &mockUserRepo{}, failQuoter{}, stubPriceChanger{})

	if _, _, err := uc.SnapshotValues(context.Background(), snapshotDay); !errors.Is(err, boom) {
		t.Fatalf("expected the repository error, got %v", err)
	}
}

func TestGetValueHistory_BoundsTheRangeFromNow(t *testing.T) {
	var gotUser string
	var gotFrom *time.Time
	repo := &mockRepo{getValueHistory: func(userID string, from *time.Time) ([]*portfoliodomain.ValuePoint, error) {
		gotUser, gotFrom = userID, from
		return []*portfoliodomain.ValuePoint{{Date: "2026-09-30", Value: 10}}, nil
	}}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	h, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, "", snapshotDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotUser != "u1" {
		t.Fatalf("expected history for u1, got %q", gotUser)
	}
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if gotFrom == nil || !gotFrom.Equal(want) {
		t.Fatalf("expected from %v (30 days back, start of day), got %v", want, gotFrom)
	}
	if h.Range != portfoliodomain.HistoryRange30D || len(h.Points) != 1 {
		t.Fatalf("expected range 30D with 1 point, got %+v", h)
	}
}

func TestGetValueHistory_AllHasNoLowerBound(t *testing.T) {
	called := false
	repo := &mockRepo{getValueHistory: func(_ string, from *time.Time) ([]*portfoliodomain.ValuePoint, error) {
		called = true
		if from != nil {
			t.Fatalf("expected no lower bound for All, got %v", from)
		}
		return []*portfoliodomain.ValuePoint{}, nil
	}}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)

	if _, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRangeAll, "", snapshotDay); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected the repository to be queried")
	}
}

// stubHistory is a stockdomain.HistoryFetcher that records its calls.
type stubHistory struct {
	closes []stockdomain.HistoryPoint
	err    error
	calls  int
	symbol string
	from   time.Time
	to     time.Time
}

func (s *stubHistory) GetHistoryClose(symbol string, from, to time.Time) ([]stockdomain.HistoryPoint, error) {
	s.calls++
	s.symbol, s.from, s.to = symbol, from, to
	return s.closes, s.err
}

var testBenchmarks = []portfoliodomain.Benchmark{{Symbol: "SPY", Label: "S&P 500"}, {Symbol: "QQQ", Label: "Nasdaq 100"}}

func benchmarkUC(points []*portfoliodomain.ValuePoint, fetcher stockdomain.HistoryFetcher) *portfoliouc.UseCase {
	repo := &mockRepo{getValueHistory: func(string, *time.Time) ([]*portfoliodomain.ValuePoint, error) { return points, nil }}
	uc := newUC(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, 10, 20)
	return uc.WithBenchmarks(fetcher, testBenchmarks)
}

func twoPoints() []*portfoliodomain.ValuePoint {
	return []*portfoliodomain.ValuePoint{{Date: "2026-09-12", Value: 10}, {Date: "2026-09-14", Value: 11}}
}

func TestGetValueHistory_BenchmarkAttachesAlignedCloses(t *testing.T) {
	f := &stubHistory{closes: []stockdomain.HistoryPoint{{Date: "2026-09-11", Close: 540.5}, {Date: "2026-09-14", Close: 545}}}
	uc := benchmarkUC(twoPoints(), f)

	h, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, "SPY", snapshotDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := portfoliodomain.BenchmarkInfo{Symbol: "SPY", Label: "S&P 500", Available: true}
	if h.Benchmark == nil || *h.Benchmark != want {
		t.Fatalf("expected %+v, got %+v", want, h.Benchmark)
	}
	if *h.Points[0].BenchmarkClose != 540.5 || *h.Points[1].BenchmarkClose != 545 {
		t.Fatalf("unexpected closes: %v %v", *h.Points[0].BenchmarkClose, *h.Points[1].BenchmarkClose)
	}
	if f.calls != 1 || f.symbol != "SPY" || !f.to.Equal(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)) || !f.from.Before(time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("unexpected fetch: calls=%d symbol=%s %v..%v", f.calls, f.symbol, f.from, f.to)
	}
}

func TestGetValueHistory_BenchmarkUnavailable(t *testing.T) {
	tests := []struct {
		name    string
		fetcher stockdomain.HistoryFetcher
	}{
		{"fetch error", &stubHistory{err: errors.New("fmp down")}},
		{"nil fetcher", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := benchmarkUC(twoPoints(), tc.fetcher)
			h, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, "SPY", snapshotDay)
			if err != nil {
				t.Fatalf("a benchmark outage must not fail the request, got %v", err)
			}
			if h.Benchmark == nil || h.Benchmark.Available || h.Benchmark.Symbol != "SPY" || h.Benchmark.Label != "S&P 500" {
				t.Fatalf("expected unavailable SPY benchmark, got %+v", h.Benchmark)
			}
			for _, p := range h.Points {
				if p.BenchmarkClose != nil {
					t.Fatalf("expected no closes, got %v", *p.BenchmarkClose)
				}
			}
		})
	}
}

func TestGetValueHistory_BenchmarkSkipsFetchWithFewerThanTwoPoints(t *testing.T) {
	for name, points := range map[string][]*portfoliodomain.ValuePoint{
		"none": {},
		"one":  {{Date: "2026-09-12", Value: 10}},
	} {
		t.Run(name, func(t *testing.T) {
			f := &stubHistory{}
			uc := benchmarkUC(points, f)
			h, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, "QQQ", snapshotDay)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if f.calls != 0 {
				t.Fatalf("expected the index provider not to be called, got %d calls", f.calls)
			}
			if h.Benchmark == nil || !h.Benchmark.Available || h.Benchmark.Symbol != "QQQ" {
				t.Fatalf("expected available QQQ info, got %+v", h.Benchmark)
			}
		})
	}
}

func TestGetValueHistory_NoBenchmarkLeavesResultUntouched(t *testing.T) {
	f := &stubHistory{}
	uc := benchmarkUC(twoPoints(), f)
	h, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, "", snapshotDay)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Benchmark != nil || f.calls != 0 || h.Points[0].BenchmarkClose != nil {
		t.Fatalf("expected no benchmark data, got %+v calls=%d", h.Benchmark, f.calls)
	}
}

func TestGetValueHistory_UnknownBenchmark(t *testing.T) {
	uc := benchmarkUC(twoPoints(), &stubHistory{})
	for _, sym := range []string{"AAPL", "spy"} {
		if _, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, sym, snapshotDay); !errors.Is(err, portfoliodomain.ErrUnknownBenchmark) {
			t.Fatalf("%s: expected ErrUnknownBenchmark, got %v", sym, err)
		}
	}
}
