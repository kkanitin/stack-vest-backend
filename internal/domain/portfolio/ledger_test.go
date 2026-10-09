package portfolio

import (
	"errors"
	"math"
	"testing"
	"time"
)

func tx(id, side, date string, qty, price, fee float64) *Transaction {
	return &Transaction{
		ID: id, Symbol: "AAPL", Side: side, Quantity: qty, Price: price, Fee: fee, Date: date,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestReplay_BuyAveragesCostWithFees(t *testing.T) {
	res, err := Replay([]*Transaction{
		tx("1", SideBuy, "2026-01-02", 10, 100, 5), // cost 1005
		tx("2", SideBuy, "2026-01-03", 10, 120, 0), // cost 1200
	})
	if err != nil {
		t.Fatal(err)
	}
	if !near(res.Shares, 20) || !near(res.AvgCost, 110.25) || !near(res.RealisedPnl, 0) {
		t.Fatalf("got %+v", res)
	}
}

func TestReplay_AvgCostUnchangedAfterSell(t *testing.T) {
	res, err := Replay([]*Transaction{
		tx("1", SideBuy, "2026-01-02", 10, 100, 0),
		tx("2", SideSell, "2026-01-03", 4, 150, 2), // realised (150-100)*4-2 = 198
	})
	if err != nil {
		t.Fatal(err)
	}
	if !near(res.Shares, 6) || !near(res.AvgCost, 100) || !near(res.RealisedPnl, 198) {
		t.Fatalf("got %+v", res)
	}
	if len(res.Rows) != 2 || !near(res.Rows[1].RunningShares, 6) || !near(res.Rows[1].RealisedPnl, 198) {
		t.Fatalf("rows %+v", res.Rows)
	}
}

func TestReplay_OversellRefused(t *testing.T) {
	_, err := Replay([]*Transaction{
		tx("1", SideBuy, "2026-01-02", 3, 100, 0),
		tx("2", SideSell, "2026-03-02", 5, 100, 0),
	})
	var ins ErrInsufficientShares
	if !errors.As(err, &ins) {
		t.Fatalf("want ErrInsufficientShares, got %v", err)
	}
	want := "This would leave you selling 5 AAPL on 2026-03-02 when you held 3."
	if err.Error() != want {
		t.Fatalf("message %q", err.Error())
	}
	if ins.Symbol != "AAPL" || ins.Date != "2026-03-02" || ins.Held != 3 || ins.Sold != 5 {
		t.Fatalf("fields %+v", ins)
	}
}

func TestReplay_EditCreatingNegativeBalanceRefused(t *testing.T) {
	txs := []*Transaction{
		tx("1", SideBuy, "2026-01-02", 10, 100, 0),
		tx("2", SideSell, "2026-02-01", 8, 110, 0),
	}
	if _, err := Replay(txs); err != nil {
		t.Fatal(err)
	}
	txs[0].Quantity = 5 // edit the earlier buy down
	if _, err := Replay(txs); !errors.As(err, new(ErrInsufficientShares)) {
		t.Fatalf("want ErrInsufficientShares, got %v", err)
	}
}

func TestReplay_SameDayBuyBeforeSell(t *testing.T) {
	// Sell listed first and created first, but same-day buys apply first.
	sell := tx("s", SideSell, "2026-01-02", 5, 100, 0)
	buy := tx("b", SideBuy, "2026-01-02", 5, 100, 0)
	sell.CreatedAt = buy.CreatedAt.Add(-time.Hour)
	res, err := Replay([]*Transaction{sell, buy})
	if err != nil {
		t.Fatal(err)
	}
	if !near(res.Shares, 0) {
		t.Fatalf("got %+v", res)
	}
}

func TestReplay_ReopenAfterFullSellStartsFreshAverage(t *testing.T) {
	res, err := Replay([]*Transaction{
		tx("1", SideBuy, "2026-01-02", 10, 100, 0),
		tx("2", SideSell, "2026-01-03", 10, 120, 0),
		tx("3", SideBuy, "2026-01-04", 5, 200, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !near(res.Shares, 5) || !near(res.AvgCost, 200) || !near(res.RealisedPnl, 200) {
		t.Fatalf("got %+v", res)
	}
}

func TestReplay_FullSellLeavesZeroAndZeroAvg(t *testing.T) {
	res, err := Replay([]*Transaction{
		tx("1", SideBuy, "2026-01-02", 0.1, 100, 0),
		tx("2", SideBuy, "2026-01-02", 0.2, 100, 0),
		tx("3", SideSell, "2026-01-03", 0.3, 100, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Shares != 0 || res.AvgCost != 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestReplay_OpeningEntryRoundTrip(t *testing.T) {
	shares, avg := 3.33333333, 187.12345678
	open := tx("o", SideBuy, "2026-01-02", shares, avg, 0)
	open.IsOpening = true
	res, err := Replay([]*Transaction{open})
	if err != nil {
		t.Fatal(err)
	}
	if res.Shares != shares || res.AvgCost != avg {
		t.Fatalf("got shares=%v avg=%v", res.Shares, res.AvgCost)
	}
}

func TestReplay_Empty(t *testing.T) {
	res, err := Replay(nil)
	if err != nil || res.Shares != 0 || res.AvgCost != 0 || len(res.Rows) != 0 {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestReplay_InvalidRows(t *testing.T) {
	for _, bad := range []*Transaction{
		tx("1", SideBuy, "2026-01-02", 0, 100, 0),
		tx("1", SideBuy, "2026-01-02", -1, 100, 0),
		tx("1", "hold", "2026-01-02", 1, 100, 0),
	} {
		if _, err := Replay([]*Transaction{bad}); !errors.Is(err, ErrInvalidTransaction) {
			t.Fatalf("want ErrInvalidTransaction, got %v", err)
		}
	}
}

func TestValidateDate(t *testing.T) {
	now := time.Date(2026, 3, 2, 23, 0, 0, 0, time.UTC)
	if err := ValidateDate("2026-03-02", now); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDate("2026-03-03", now); !errors.Is(err, ErrFutureDate) {
		t.Fatalf("got %v", err)
	}
	if err := ValidateDate("nope", now); !errors.Is(err, ErrInvalidTransaction) {
		t.Fatalf("got %v", err)
	}
}
