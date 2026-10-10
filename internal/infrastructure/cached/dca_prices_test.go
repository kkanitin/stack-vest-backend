package cached

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
)

type countingPriceFetcher struct {
	calls  atomic.Int32
	prices []dca.HistoricalPrice
	err    error
}

func (f *countingPriceFetcher) GetHistoricalPrices(_ string, _, _ time.Time) ([]dca.HistoricalPrice, error) {
	f.calls.Add(1)
	return f.prices, f.err
}

func TestDCAPrices_ReusesSameSymbolAndRange(t *testing.T) {
	inner := &countingPriceFetcher{prices: []dca.HistoricalPrice{{Date: day("2024-01-02"), Close: 10}}}
	p := NewDCAPrices(inner, time.Hour)

	for _, sym := range []string{"AAPL", "AAPL", "aapl"} {
		got, err := p.GetHistoricalPrices(sym, day("2024-01-01"), day("2024-02-01"))
		if err != nil || len(got) != 1 || got[0].Close != 10 {
			t.Fatalf("got %v, %v", got, err)
		}
	}
	if n := inner.calls.Load(); n != 1 {
		t.Fatalf("provider calls = %d, want 1", n)
	}
}

func TestDCAPrices_DifferentSymbolOrRangeFetchesAgain(t *testing.T) {
	inner := &countingPriceFetcher{prices: []dca.HistoricalPrice{{Date: day("2024-01-02"), Close: 10}}}
	p := NewDCAPrices(inner, time.Hour)

	p.GetHistoricalPrices("AAPL", day("2024-01-01"), day("2024-02-01"))
	p.GetHistoricalPrices("MSFT", day("2024-01-01"), day("2024-02-01"))
	p.GetHistoricalPrices("AAPL", day("2024-01-01"), day("2024-03-01"))
	p.GetHistoricalPrices("AAPL", day("2024-01-05"), day("2024-02-01"))
	if n := inner.calls.Load(); n != 4 {
		t.Fatalf("provider calls = %d, want 4", n)
	}
}

func TestDCAPrices_DoesNotCacheErrors(t *testing.T) {
	inner := &countingPriceFetcher{err: errors.New("upstream down")}
	p := NewDCAPrices(inner, time.Hour)

	if _, err := p.GetHistoricalPrices("AAPL", day("2024-01-01"), day("2024-02-01")); err == nil {
		t.Fatal("want error")
	}
	inner.err = nil
	inner.prices = []dca.HistoricalPrice{{Date: day("2024-01-02"), Close: 10}}
	got, err := p.GetHistoricalPrices("AAPL", day("2024-01-01"), day("2024-02-01"))
	if err != nil || len(got) != 1 {
		t.Fatalf("retry got %v, %v", got, err)
	}
	if n := inner.calls.Load(); n != 2 {
		t.Fatalf("provider calls = %d, want 2", n)
	}
}

func TestDCAPrices_ReturnsCopy(t *testing.T) {
	inner := &countingPriceFetcher{prices: []dca.HistoricalPrice{{Date: day("2024-01-02"), Close: 10}}}
	p := NewDCAPrices(inner, time.Hour)

	first, _ := p.GetHistoricalPrices("AAPL", day("2024-01-01"), day("2024-02-01"))
	first[0].Close = 999
	second, _ := p.GetHistoricalPrices("AAPL", day("2024-01-01"), day("2024-02-01"))
	if second[0].Close != 10 {
		t.Fatalf("cache was mutated through returned slice: %v", second[0].Close)
	}
}
