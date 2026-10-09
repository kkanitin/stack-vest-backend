package portfolio_test

import (
	"context"
	"errors"
	"math"
	"sort"
	"testing"
	"time"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
)

// rtLedgerRepo layers ledger data over mockRepo.
type rtLedgerRepo struct {
	*mockRepo
	txs []*portfoliodomain.Transaction
	err error
}

func (r *rtLedgerRepo) ListTransactions(_ context.Context, portfolioID, symbol string) ([]*portfoliodomain.Transaction, error) {
	var out []*portfoliodomain.Transaction
	for _, t := range r.txs {
		if t.PortfolioID == portfolioID && (symbol == "" || t.Symbol == symbol) {
			out = append(out, t)
		}
	}
	return out, r.err
}
func (r *rtLedgerRepo) ListTransactionsByUser(context.Context, string) ([]*portfoliodomain.Transaction, error) {
	return r.txs, r.err
}

// rtCloses serves explicit daily closes per symbol, filtered to the asked range.
type rtCloses struct {
	closes map[string]map[string]float64 // symbol -> date -> close
	err    error
}

func (f rtCloses) GetHistoryClose(symbol string, from, to time.Time) ([]stockdomain.HistoryPoint, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []stockdomain.HistoryPoint
	for d := from.UTC().Truncate(24 * time.Hour); !d.After(to); d = d.AddDate(0, 0, 1) {
		if c, ok := f.closes[symbol][d.Format("2006-01-02")]; ok {
			out = append(out, stockdomain.HistoryPoint{Date: d.Format("2006-01-02"), Close: c})
		}
	}
	return out, nil
}

func rtDay(offset int) string {
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, offset).Format("2006-01-02")
}

// rtStep is `before` until rtDay(-6), then `after`, for every day from rtDay(-60).
func rtStep(before, after float64) map[string]float64 {
	m := map[string]float64{}
	for i := -60; i <= 0; i++ {
		if i < -5 {
			m[rtDay(i)] = before
		} else {
			m[rtDay(i)] = after
		}
	}
	return m
}

func rtTx(pf, sym, side string, qty, price float64, date string, opening bool) *portfoliodomain.Transaction {
	return &portfoliodomain.Transaction{
		ID: sym + date + side, PortfolioID: pf, Symbol: sym, Side: side, Quantity: qty, Price: price,
		Date: date, IsOpening: opening, CreatedAt: time.Now(),
	}
}

func rtUC(repo portfoliodomain.Repository, quote float64, fetcher stockdomain.HistoryFetcher) *portfoliouc.UseCase {
	q := stubQuoter{price: map[string]float64{"AAPL": quote, "MSFT": quote}}
	uc := portfoliouc.New(repo, &mockUserRepo{user: &userdomain.User{ID: "u1"}}, q, stubPriceChanger{}, 10, 20)
	return uc.WithBenchmarks(fetcher, nil)
}

func rtRepo(positions []*portfoliodomain.Position, txs []*portfoliodomain.Transaction) *rtLedgerRepo {
	return &rtLedgerRepo{
		txs: txs,
		mockRepo: &mockRepo{
			getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
				return &portfoliodomain.Portfolio{ID: id, UserID: "u1"}, nil
			},
			listByPortfolioID:   func(string) ([]*portfoliodomain.Position, error) { return positions, nil },
			listPositionsByUser: func(string) ([]*portfoliodomain.Position, error) { return positions, nil },
		},
	}
}

func rtApprox(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestGetSummary_BuyAtCurrentPriceLeavesTWRUnchanged(t *testing.T) {
	// 10 AAPL bought at 100 (price jumped to 110 5 days ago), then 10 more at 110.
	txs := []*portfoliodomain.Transaction{
		rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false),
		rtTx("pf1", "AAPL", "buy", 10, 110, rtDay(-3), false),
	}
	positions := []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 20, AvgCost: 105}}
	f := rtCloses{closes: map[string]map[string]float64{"AAPL": rtStep(100, 110)}}
	uc := rtUC(rtRepo(positions, txs), 110, f)

	s, err := uc.GetSummary(context.Background(), "a@b.com", "pf1")
	if err != nil {
		t.Fatal(err)
	}
	if !rtApprox(s.ChangePct30d, 10) {
		t.Fatalf("changePct30d = %v, want 10", s.ChangePct30d)
	}
	if !rtApprox(s.Change30d, 100) {
		t.Fatalf("change30d = %v, want 100", s.Change30d)
	}
	if !rtApprox(s.TotalValue, 2200) {
		t.Fatalf("totalValue = %v", s.TotalValue)
	}
	if !rtApprox(s.UnrealisedPnl, 100) || s.RealisedPnl != 0 {
		t.Fatalf("pnl = %v / %v", s.UnrealisedPnl, s.RealisedPnl)
	}
}

func TestGetSummary_OpeningEntryIsNotAGain(t *testing.T) {
	// Carried-over holding appears mid-window at avg cost 50 (market 100): no jump.
	txs := []*portfoliodomain.Transaction{rtTx("pf1", "AAPL", "buy", 10, 50, rtDay(-10), true)}
	positions := []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 10, AvgCost: 50}}
	f := rtCloses{closes: map[string]map[string]float64{"AAPL": rtStep(100, 100)}}
	uc := rtUC(rtRepo(positions, txs), 100, f)

	s, err := uc.GetSummary(context.Background(), "a@b.com", "pf1")
	if err != nil {
		t.Fatal(err)
	}
	if !rtApprox(s.ChangePct30d, 0) || !rtApprox(s.Change30d, 0) {
		t.Fatalf("got %v / %v, want 0", s.ChangePct30d, s.Change30d)
	}
	if !rtApprox(s.UnrealisedPnl, 500) {
		t.Fatalf("unrealised = %v", s.UnrealisedPnl)
	}
}

func TestGetSummary_RealisedFromLedgerIncludingClosedHoldings(t *testing.T) {
	txs := []*portfoliodomain.Transaction{
		rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false),
		rtTx("pf1", "AAPL", "sell", 5, 120, rtDay(-20), false), // +100
		rtTx("pf1", "MSFT", "buy", 1, 10, rtDay(-50), false),
		rtTx("pf1", "MSFT", "sell", 1, 15, rtDay(-40), false), // +5, closed
	}
	positions := []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 5, AvgCost: 100}}
	f := rtCloses{closes: map[string]map[string]float64{"AAPL": rtStep(120, 120)}}
	uc := rtUC(rtRepo(positions, txs), 120, f)

	s, err := uc.GetSummary(context.Background(), "a@b.com", "pf1")
	if err != nil {
		t.Fatal(err)
	}
	if !rtApprox(s.RealisedPnl, 105) {
		t.Fatalf("realised = %v", s.RealisedPnl)
	}
	if !rtApprox(s.UnrealisedPnl, 100) {
		t.Fatalf("unrealised = %v", s.UnrealisedPnl)
	}
	if !rtApprox(s.ChangePct30d, 0) {
		t.Fatalf("changePct30d = %v", s.ChangePct30d)
	}
}

func TestGetSummary_NoHistoryFetcherOrFailingFetcherReturnsZeroReturn(t *testing.T) {
	txs := []*portfoliodomain.Transaction{rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false)}
	positions := []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 10, AvgCost: 100}}
	for name, f := range map[string]stockdomain.HistoryFetcher{
		"nil":     nil,
		"failing": rtCloses{err: errors.New("down")},
	} {
		t.Run(name, func(t *testing.T) {
			uc := rtUC(rtRepo(positions, txs), 150, f)
			s, err := uc.GetSummary(context.Background(), "a@b.com", "pf1")
			if err != nil {
				t.Fatal(err)
			}
			if s.ChangePct30d != 0 || s.Change30d != 0 {
				t.Fatalf("got %v / %v, want 0", s.ChangePct30d, s.Change30d)
			}
			if !rtApprox(s.TotalValue, 1500) || !rtApprox(s.UnrealisedPnl, 500) {
				t.Fatalf("value/unrealised = %v / %v", s.TotalValue, s.UnrealisedPnl)
			}
		})
	}
}

func TestGetSummary_ForwardFillsMissingCloses(t *testing.T) {
	// Closes exist only on a few days (weekends and holidays have none): still 100 -> 110.
	closes := map[string]float64{rtDay(-45): 100, rtDay(-8): 100, rtDay(-1): 110}
	txs := []*portfoliodomain.Transaction{rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false)}
	positions := []*portfoliodomain.Position{{PortfolioID: "pf1", Symbol: "AAPL", Shares: 10, AvgCost: 100}}
	uc := rtUC(rtRepo(positions, txs), 110, rtCloses{closes: map[string]map[string]float64{"AAPL": closes}})

	s, err := uc.GetSummary(context.Background(), "a@b.com", "pf1")
	if err != nil {
		t.Fatal(err)
	}
	if !rtApprox(s.ChangePct30d, 10) {
		t.Fatalf("changePct30d = %v", s.ChangePct30d)
	}
}

func TestGetPortfoliosSummary_TWRAndPnlAcrossPortfolios(t *testing.T) {
	txs := []*portfoliodomain.Transaction{
		rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false),
		rtTx("pf2", "MSFT", "buy", 10, 100, rtDay(-50), false),
		rtTx("pf2", "MSFT", "buy", 10, 110, rtDay(-2), false),
		rtTx("pf2", "MSFT", "sell", 2, 130, rtDay(-1), false), // realised (130-105)*2 = 50
	}
	positions := []*portfoliodomain.Position{
		{PortfolioID: "pf1", Symbol: "AAPL", Shares: 10, AvgCost: 100},
		{PortfolioID: "pf2", Symbol: "MSFT", Shares: 18, AvgCost: 105},
	}
	f := rtCloses{closes: map[string]map[string]float64{
		"AAPL": rtStep(100, 110), "MSFT": rtStep(100, 110),
	}}
	uc := rtUC(rtRepo(positions, txs), 110, f)

	s, err := uc.GetPortfoliosSummary(context.Background(), "a@b.com")
	if err != nil {
		t.Fatal(err)
	}
	// 10% from the price step, compounded with the 130-vs-110 sell on day -1:
	// (3080+260)/3300 = 1.0121, so 1.1 * 1.0121 - 1 = 11.33%.
	if s.ChangePct < 11.33 || s.ChangePct > 11.34 {
		t.Fatalf("changePct = %v", s.ChangePct)
	}
	if !rtApprox(s.RealisedPnl, 50) {
		t.Fatalf("realised = %v", s.RealisedPnl)
	}
	if !rtApprox(s.UnrealisedPnl, 10*10+18*5) {
		t.Fatalf("unrealised = %v", s.UnrealisedPnl)
	}
}

func TestGetValueHistory_ReturnPctIsCumulativeTWRFromFirstPoint(t *testing.T) {
	now := time.Now().UTC()
	// Price 100 until rtDay(-6), 110 from rtDay(-5). Snapshots on -6, -5 and today. A deposit
	// at the current price on rtDay(-3) must not move the return.
	txs := []*portfoliodomain.Transaction{
		rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false),
		rtTx("pf1", "AAPL", "buy", 10, 110, rtDay(-3), false),
	}
	repo := rtRepo(nil, txs)
	repo.getValueHistory = func(string, *time.Time) ([]*portfoliodomain.ValuePoint, error) {
		return []*portfoliodomain.ValuePoint{
			{Date: rtDay(-6), Value: 1000}, {Date: rtDay(-5), Value: 1100}, {Date: rtDay(0), Value: 2200},
		}, nil
	}
	f := rtCloses{closes: map[string]map[string]float64{"AAPL": rtStep(100, 110)}}
	uc := rtUC(repo, 110, f)

	h, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, "", now)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0, 10, 10}
	for i, p := range h.Points {
		if !rtApprox(p.ReturnPct, want[i]) {
			t.Fatalf("point %d returnPct = %v, want %v", i, p.ReturnPct, want[i])
		}
	}
	if h.Points[2].Value != 2200 {
		t.Fatalf("value must stay the snapshot value, got %v", h.Points[2].Value)
	}
}

func TestGetValueHistory_ReturnPctZeroWithoutLedgerOrFetcher(t *testing.T) {
	repo := rtRepo(nil, nil)
	repo.getValueHistory = func(string, *time.Time) ([]*portfoliodomain.ValuePoint, error) {
		return []*portfoliodomain.ValuePoint{{Date: rtDay(-2), Value: 100}, {Date: rtDay(0), Value: 120}}, nil
	}
	uc := rtUC(repo, 110, nil)
	h, err := uc.GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range h.Points {
		if p.ReturnPct != 0 {
			t.Fatalf("returnPct = %v, want 0", p.ReturnPct)
		}
	}
}

// rtFlat is a constant close for every day of the test window.
func rtFlat(price float64) map[string]float64 { return rtStep(price, price) }

// rtAt stamps a transaction with an explicit unique id and creation time.
func rtAt(tx *portfoliodomain.Transaction, id string, created time.Time) *portfoliodomain.Transaction {
	tx.ID, tx.CreatedAt = id, created
	return tx
}

// rtReturnPcts runs GetValueHistory over txs, listed the way the repository returns them
// (trade_date DESC, created_at DESC), and returns the ReturnPct of points at the offsets.
func rtReturnPcts(t *testing.T, txs []*portfoliodomain.Transaction, offsets ...int) []float64 {
	t.Helper()
	listed := make([]*portfoliodomain.Transaction, len(txs))
	copy(listed, txs)
	sort.SliceStable(listed, func(i, j int) bool {
		if listed[i].Date != listed[j].Date {
			return listed[i].Date > listed[j].Date
		}
		return listed[i].CreatedAt.After(listed[j].CreatedAt)
	})
	repo := rtRepo(nil, listed)
	repo.getValueHistory = func(string, *time.Time) ([]*portfoliodomain.ValuePoint, error) {
		var pts []*portfoliodomain.ValuePoint
		for _, o := range offsets {
			pts = append(pts, &portfoliodomain.ValuePoint{Date: rtDay(o), Value: 1000})
		}
		return pts, nil
	}
	f := rtCloses{closes: map[string]map[string]float64{"AAPL": rtFlat(100), "MSFT": rtFlat(100)}}
	h, err := rtUC(repo, 100, f).GetValueHistory(context.Background(), "a@b.com", portfoliodomain.HistoryRange30D, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float64, len(h.Points))
	for i, p := range h.Points {
		out[i] = p.ReturnPct
	}
	return out
}

func rtAssertFlat(t *testing.T, got []float64) {
	t.Helper()
	for i, v := range got {
		if !rtApprox(v, 0) {
			t.Fatalf("point %d returnPct = %v, want 0 (held-share series corrupted): %v", i, v, got)
		}
	}
}

func TestSeries_SameDayBuyAndSellFromZeroHeld(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	txs := []*portfoliodomain.Transaction{
		rtAt(rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false), "a0", base),
		rtAt(rtTx("pf1", "MSFT", "buy", 10, 100, rtDay(-3), false), "m1", base.Add(time.Minute)),
		rtAt(rtTx("pf1", "MSFT", "sell", 10, 100, rtDay(-3), false), "m2", base.Add(2*time.Minute)),
	}
	rtAssertFlat(t, rtReturnPcts(t, txs, -6, -4, -2, 0))
}

func TestSeries_SameDayBuyAndSellWithHolding(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	txs := []*portfoliodomain.Transaction{
		rtAt(rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false), "a0", base),
		rtAt(rtTx("pf1", "MSFT", "buy", 5, 100, rtDay(-50), false), "m0", base),
		rtAt(rtTx("pf1", "MSFT", "buy", 10, 100, rtDay(-3), false), "m1", base.Add(time.Minute)),
		rtAt(rtTx("pf1", "MSFT", "sell", 10, 100, rtDay(-3), false), "m2", base.Add(2*time.Minute)),
	}
	rtAssertFlat(t, rtReturnPcts(t, txs, -6, -4, -2, 0))
}

func TestSeries_SameSymbolTwoPortfoliosSameDay(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	txs := []*portfoliodomain.Transaction{
		rtAt(rtTx("pf1", "AAPL", "buy", 10, 100, rtDay(-50), false), "a0", base),
		rtAt(rtTx("pf1", "MSFT", "buy", 10, 100, rtDay(-50), false), "m0", base),
		rtAt(rtTx("pf2", "MSFT", "buy", 10, 100, rtDay(-3), false), "m1", base.Add(time.Minute)),
		rtAt(rtTx("pf2", "MSFT", "sell", 10, 100, rtDay(-3), false), "m2", base.Add(2*time.Minute)),
		rtAt(rtTx("pf1", "MSFT", "sell", 10, 100, rtDay(-3), false), "m3", base.Add(3*time.Minute)),
	}
	rtAssertFlat(t, rtReturnPcts(t, txs, -6, -4, -2, 0))
}
