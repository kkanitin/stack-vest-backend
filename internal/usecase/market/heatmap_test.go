package marketuc

import (
	"context"
	"errors"
	"testing"

	domain "github.com/kanitin/stackvest/backend/internal/domain/market"
	stockdomain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

type fakeLister struct {
	byIndex map[domain.Index][]domain.Constituent
	err     error
}

func (f *fakeLister) ListConstituents(index domain.Index) ([]domain.Constituent, error) {
	return f.byIndex[index], f.err
}

type fakeQuoter struct{ quotes map[string]domain.Quote }

func (f *fakeQuoter) GetBatchQuotes(symbols []string) ([]domain.Quote, error) {
	var out []domain.Quote
	for _, s := range symbols {
		if q, ok := f.quotes[s]; ok {
			out = append(out, q)
		}
	}
	return out, nil
}

type fakeChanger struct{ failing map[string]bool }

func (f *fakeChanger) GetPriceChange(symbol string) (*stockdomain.PriceChange, error) {
	if f.failing[symbol] {
		return nil, errors.New("boom")
	}
	return &stockdomain.PriceChange{Symbol: symbol, D5: 2, M1: 3, YTD: 4}, nil
}

func newTestUC(lister *fakeLister) *HeatmapUseCase {
	quoter := &fakeQuoter{quotes: map[string]domain.Quote{
		"AAPL": {Symbol: "AAPL", Price: 200, ChangePercent: 1, MarketCap: 3000},
		"MSFT": {Symbol: "MSFT", Price: 400, ChangePercent: -1, MarketCap: 3500},
		"JPM":  {Symbol: "JPM", Price: 150, ChangePercent: 0.5, MarketCap: 500},
		"ZERO": {Symbol: "ZERO", Price: 1, MarketCap: 0},
		"ODD":  {Symbol: "ODD", Price: 1, MarketCap: 10},
	}}
	return NewHeatmapUseCase(lister, quoter, &fakeChanger{failing: map[string]bool{"JPM": true}})
}

func TestGetBeforeRefreshIsNotReady(t *testing.T) {
	uc := newTestUC(&fakeLister{})
	if _, err := uc.Get(domain.IndexSP500); !errors.Is(err, domain.ErrHeatmapNotReady) {
		t.Fatalf("expected ErrHeatmapNotReady, got %v", err)
	}
}

func TestRefreshGroupsAndSorts(t *testing.T) {
	uc := newTestUC(&fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexSP500: {
			{Symbol: "JPM", Name: "JPMorgan", Sector: "Financials"},
			{Symbol: "AAPL", Name: "Apple", Sector: "Technology"},
			{Symbol: "MSFT", Name: "Microsoft", Sector: "Technology"},
			{Symbol: "ZERO", Name: "No cap", Sector: "Technology"},
			{Symbol: "NOQUOTE", Name: "Missing", Sector: "Technology"},
			{Symbol: "ODD", Name: "No sector"},
		},
	}})
	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hm, err := uc.Get(domain.IndexSP500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hm.Sectors) != 3 {
		t.Fatalf("expected 3 sectors, got %+v", hm.Sectors)
	}
	tech := hm.Sectors[0]
	if tech.Name != "Technology" || tech.MarketCap != 6500 || len(tech.Stocks) != 2 {
		t.Fatalf("unexpected first sector %+v", tech)
	}
	if tech.Stocks[0].Symbol != "MSFT" || tech.Stocks[1].Symbol != "AAPL" {
		t.Errorf("stocks not sorted by market cap: %+v", tech.Stocks)
	}
	if hm.Sectors[1].Name != "Financials" || hm.Sectors[2].Name != otherSector {
		t.Errorf("sectors not sorted by market cap: %s, %s", hm.Sectors[1].Name, hm.Sectors[2].Name)
	}

	msft := tech.Stocks[0].Change
	if *msft.D1 != -1 || *msft.W1 != 2 || *msft.M1 != 3 || *msft.YTD != 4 {
		t.Errorf("unexpected MSFT change %+v", msft)
	}
	jpm := hm.Sectors[1].Stocks[0].Change
	if jpm.D1 == nil || *jpm.D1 != 0.5 || jpm.W1 != nil || jpm.YTD != nil {
		t.Errorf("failed price change should leave only 1D: %+v", jpm)
	}
}

func TestRefreshKeepsPreviousSnapshotOnFailure(t *testing.T) {
	lister := &fakeLister{byIndex: map[domain.Index][]domain.Constituent{
		domain.IndexDow30: {{Symbol: "AAPL", Sector: "Technology"}},
	}}
	uc := newTestUC(lister)
	if err := uc.Refresh(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	first, _ := uc.Get(domain.IndexDow30)

	lister.err = errors.New("upstream down")
	if err := uc.Refresh(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if got, _ := uc.Get(domain.IndexDow30); got != first {
		t.Error("previous snapshot should be kept after a failed refresh")
	}
}

func TestRefreshStopsWhenCancelled(t *testing.T) {
	uc := newTestUC(&fakeLister{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := uc.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
