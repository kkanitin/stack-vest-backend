package portfolio_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	dcadomain "github.com/kanitin/stackvest/backend/internal/domain/dca"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
)

// srNow is the fixed "today" for the suggested-return tests; the ten-year window starts on
// 2016-06-15.
var srNow = time.Date(2026, 6, 15, 14, 0, 0, 0, time.UTC)

// stubLongHistory serves fixed closes (or an error) per symbol and records the window it
// was asked for.
type stubLongHistory struct {
	closes   map[string][]dcadomain.HistoricalPrice
	errs     map[string]error
	from, to time.Time
}

func (s *stubLongHistory) GetHistoricalPrices(symbol string, from, to time.Time) ([]dcadomain.HistoricalPrice, error) {
	s.from, s.to = from, to
	if err := s.errs[symbol]; err != nil {
		return nil, err
	}
	c, ok := s.closes[symbol]
	if !ok {
		return nil, dcadomain.ErrSymbolNotFound
	}
	return c, nil
}

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// series returns closes on the given dates (oldest first) with the given values.
func series(points ...any) []dcadomain.HistoricalPrice {
	out := make([]dcadomain.HistoricalPrice, 0, len(points)/2)
	for i := 0; i < len(points); i += 2 {
		out = append(out, dcadomain.HistoricalPrice{Date: day(points[i].(string)), Close: points[i+1].(float64)})
	}
	return out
}

func srUseCase(positions []*portfoliodomain.Position, prices map[string]float64, history *stubLongHistory, owner string) (*portfoliouc.UseCase, *mockRepo) {
	repo := &mockRepo{
		getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
			return &portfoliodomain.Portfolio{ID: id, UserID: owner}, nil
		},
		listByPortfolioID: func(string) ([]*portfoliodomain.Position, error) { return positions, nil },
	}
	uc := portfoliouc.New(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, selectiveQuoter{price: prices}, nil, 10, 20)
	if history != nil {
		uc = uc.WithLongHistory(history)
	}
	return uc, repo
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestSuggestedReturn_WeightsByCurrentValue(t *testing.T) {
	// AAA doubles over ten years (≈7.18%/yr); BBB is flat (0%/yr). AAA is worth 300, BBB 100,
	// so the suggestion is 0.75 × 7.18 ≈ 5.38%.
	history := &stubLongHistory{closes: map[string][]dcadomain.HistoricalPrice{
		"AAA": series("2016-06-15", 50.0, "2021-06-15", 70.0, "2026-06-15", 100.0),
		"BBB": series("2016-06-15", 20.0, "2026-06-15", 20.0),
	}}
	positions := []*portfoliodomain.Position{
		{Symbol: "BBB", Name: "Bee", Shares: 5},
		{Symbol: "AAA", Name: "Ay", Shares: 3},
	}
	uc, _ := srUseCase(positions, map[string]float64{"AAA": 100, "BBB": 20}, history, "u1")

	got, err := uc.SuggestedReturn(context.Background(), "a@b.com", "pf1", srNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Available || got.AnnualReturn == nil {
		t.Fatalf("expected a suggestion, got %+v", got)
	}
	aaaCAGR := (math.Pow(2, 1/(3652.0/365.25)) - 1) * 100
	if want := 0.75 * aaaCAGR; !near(*got.AnnualReturn, want, 0.01) {
		t.Fatalf("annualReturn = %v, want ≈ %v", *got.AnnualReturn, want)
	}
	if len(got.Holdings) != 2 || got.Holdings[0].Symbol != "AAA" {
		t.Fatalf("holdings should be ordered by value, AAA first: %+v", got.Holdings)
	}
	if got.Holdings[0].WeightPct != 75 || got.Holdings[1].WeightPct != 25 {
		t.Fatalf("weights = %v / %v, want 75 / 25", got.Holdings[0].WeightPct, got.Holdings[1].WeightPct)
	}
	if !near(got.Holdings[0].AnnualReturn, aaaCAGR, 0.01) || got.Holdings[1].AnnualReturn != 0 {
		t.Fatalf("per-holding returns = %v / %v", got.Holdings[0].AnnualReturn, got.Holdings[1].AnnualReturn)
	}
	lb := got.Lookback
	if lb == nil || lb.StartDate != "2016-06-15" || lb.EndDate != "2026-06-15" || lb.RequestedYears != 10 || lb.ShortHistory {
		t.Fatalf("lookback = %+v", lb)
	}
	if !near(lb.Years, 10, 0.01) {
		t.Fatalf("lookback years = %v, want ≈ 10", lb.Years)
	}
	if len(got.Skipped) != 0 {
		t.Fatalf("nothing should be skipped: %+v", got.Skipped)
	}
	if !history.from.Equal(day("2016-06-15")) || !history.to.Equal(day("2026-06-15")) {
		t.Fatalf("history window = %v..%v, want 2016-06-15..2026-06-15", history.from, history.to)
	}
}

func TestSuggestedReturn_ShortHistoryUsesWhatIsAvailable(t *testing.T) {
	// NEW listed in mid-2022 and has risen 21% over its four years: 10%/yr for 2 years would
	// be 21%, so ~4 years gives ≈ 4.88%/yr.
	history := &stubLongHistory{closes: map[string][]dcadomain.HistoricalPrice{
		"OLD": series("2016-06-15", 10.0, "2026-06-15", 10.0),
		"NEW": series("2022-06-15", 100.0, "2026-06-15", 121.0),
	}}
	positions := []*portfoliodomain.Position{{Symbol: "OLD", Shares: 1}, {Symbol: "NEW", Shares: 1}}
	uc, _ := srUseCase(positions, map[string]float64{"OLD": 100, "NEW": 100}, history, "u1")

	got, err := uc.SuggestedReturn(context.Background(), "a@b.com", "pf1", srNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var newH *portfoliodomain.SuggestedReturnHolding
	for i := range got.Holdings {
		if got.Holdings[i].Symbol == "NEW" {
			newH = &got.Holdings[i]
		} else if got.Holdings[i].ShortHistory {
			t.Fatalf("OLD has the full ten years and must not be flagged: %+v", got.Holdings[i])
		}
	}
	if newH == nil || !newH.ShortHistory || newH.StartDate != "2022-06-15" || !near(newH.Years, 4, 0.01) {
		t.Fatalf("NEW should use its four years and be flagged short: %+v", newH)
	}
	wantNew := (math.Pow(1.21, 1/(1461.0/365.25)) - 1) * 100
	if !near(newH.AnnualReturn, wantNew, 0.01) {
		t.Fatalf("NEW return = %v, want ≈ %v", newH.AnnualReturn, wantNew)
	}
	if !got.Lookback.ShortHistory || got.Lookback.StartDate != "2016-06-15" {
		t.Fatalf("lookback should span the longest history and be flagged short: %+v", got.Lookback)
	}
	if got.Note == "" {
		t.Fatal("a short-history note was expected")
	}
}

func TestSuggestedReturn_SkippedHoldings(t *testing.T) {
	history := &stubLongHistory{
		closes: map[string][]dcadomain.HistoricalPrice{
			"GOOD":  series("2016-06-15", 10.0, "2026-06-15", 20.0),
			"YOUNG": series("2026-01-02", 10.0, "2026-06-15", 12.0), // under a year
			"NOQ":   series("2016-06-15", 10.0, "2026-06-15", 20.0),
		},
		errs: map[string]error{"DOWN": errors.New("provider timeout")},
	}
	positions := []*portfoliodomain.Position{
		{Symbol: "GOOD", Shares: 1},
		{Symbol: "NOQ", Shares: 1},  // no live quote
		{Symbol: "GONE", Shares: 1}, // no history at all
		{Symbol: "YOUNG", Shares: 1},
		{Symbol: "DOWN", Shares: 1},
		{Symbol: "SOLD", Shares: 0}, // closed holding: not considered
	}
	prices := map[string]float64{"GOOD": 20, "GONE": 5, "YOUNG": 12, "DOWN": 7, "SOLD": 1}
	uc, _ := srUseCase(positions, prices, history, "u1")

	got, err := uc.SuggestedReturn(context.Background(), "a@b.com", "pf1", srNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Available || len(got.Holdings) != 1 || got.Holdings[0].Symbol != "GOOD" || got.Holdings[0].WeightPct != 100 {
		t.Fatalf("only GOOD should be used, at full weight: %+v", got.Holdings)
	}
	if *got.AnnualReturn != got.Holdings[0].AnnualReturn {
		t.Fatalf("one holding: annualReturn %v should equal its return %v", *got.AnnualReturn, got.Holdings[0].AnnualReturn)
	}
	want := map[string]string{
		"NOQ":   portfoliodomain.SkipNoPrice,
		"GONE":  portfoliodomain.SkipNoHistory,
		"YOUNG": portfoliodomain.SkipNoHistory,
		"DOWN":  portfoliodomain.SkipHistoryUnavailable,
	}
	if len(got.Skipped) != len(want) {
		t.Fatalf("skipped = %+v, want %v", got.Skipped, want)
	}
	for _, s := range got.Skipped {
		if want[s.Symbol] != s.Code || s.Reason == "" {
			t.Fatalf("skip %+v, want code %q with a reason", s, want[s.Symbol])
		}
	}
}

func TestSuggestedReturn_NoPricedHoldings(t *testing.T) {
	history := &stubLongHistory{closes: map[string][]dcadomain.HistoricalPrice{
		"AAA": series("2016-06-15", 10.0, "2026-06-15", 20.0),
	}}
	positions := []*portfoliodomain.Position{{Symbol: "AAA", Shares: 1}, {Symbol: "BBB", Shares: 1}}
	uc, _ := srUseCase(positions, map[string]float64{}, history, "u1")

	got, err := uc.SuggestedReturn(context.Background(), "a@b.com", "pf1", srNow)
	if err != nil {
		t.Fatalf("no priced holdings must not be an error, got %v", err)
	}
	if got.Available || got.AnnualReturn != nil || got.Lookback != nil || len(got.Holdings) != 0 || got.Note == "" {
		t.Fatalf("expected a no-suggestion result with a note, got %+v", got)
	}
	if len(got.Skipped) != 2 || got.Skipped[0].Code != portfoliodomain.SkipNoPrice {
		t.Fatalf("both holdings should be skipped for no price: %+v", got.Skipped)
	}
}

func TestSuggestedReturn_EmptyPortfolio(t *testing.T) {
	uc, _ := srUseCase(nil, nil, &stubLongHistory{}, "u1")

	got, err := uc.SuggestedReturn(context.Background(), "a@b.com", "pf1", srNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Available || got.AnnualReturn != nil || got.Holdings == nil || got.Skipped == nil || got.Note == "" {
		t.Fatalf("expected an empty no-suggestion result, got %+v", got)
	}
}

func TestSuggestedReturn_NoHistoryFetcher(t *testing.T) {
	positions := []*portfoliodomain.Position{{Symbol: "AAA", Shares: 1}}
	uc, _ := srUseCase(positions, map[string]float64{"AAA": 10}, nil, "u1")

	got, err := uc.SuggestedReturn(context.Background(), "a@b.com", "pf1", srNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Available || len(got.Skipped) != 1 || got.Skipped[0].Code != portfoliodomain.SkipHistoryUnavailable {
		t.Fatalf("expected AAA skipped as history unavailable, got %+v", got)
	}
}

func TestSuggestedReturn_NotOwned(t *testing.T) {
	history := &stubLongHistory{}
	positions := []*portfoliodomain.Position{{Symbol: "AAA", Shares: 1}}
	uc, _ := srUseCase(positions, map[string]float64{"AAA": 10}, history, "someone-else")

	_, err := uc.SuggestedReturn(context.Background(), "a@b.com", "pf1", srNow)
	if !errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		t.Fatalf("expected ErrPortfolioNotFound (404), got %v", err)
	}
	if !history.from.IsZero() {
		t.Fatal("no price history should be fetched for a portfolio the user does not own")
	}
}
