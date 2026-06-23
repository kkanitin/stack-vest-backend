package stockuc_test

import (
	"errors"
	"testing"
	"time"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
)

type mockSearcher struct {
	symbolMatches []domain.Match
	symbolErr     error
	nameMatches   []domain.Match
	nameErr       error

	stockSymbols []string
	etfSymbols   []string
	listErr      error
}

func (m *mockSearcher) SearchSymbol(string) ([]domain.Match, error) {
	return m.symbolMatches, m.symbolErr
}

func (m *mockSearcher) SearchName(string) ([]domain.Match, error) {
	return m.nameMatches, m.nameErr
}

func (m *mockSearcher) ListStockSymbols() ([]string, error) {
	return m.stockSymbols, m.listErr
}

func (m *mockSearcher) ListETFSymbols() ([]string, error) {
	return m.etfSymbols, m.listErr
}

func TestSearchUseCase_Execute(t *testing.T) {
	goog := domain.Match{Symbol: "GOOGL", Name: "Alphabet Inc."}
	aapl := domain.Match{Symbol: "AAPL", Name: "Apple Inc."}
	spy := domain.Match{Symbol: "SPY", Name: "SPDR S&P 500 ETF Trust"}
	index := domain.Match{Symbol: "^GSPC", Name: "S&P 500"}

	// A non-empty universe so filtering is active. Stocks and ETFs are kept
	// separate; membership in either passes the filter.
	stocks := []string{"GOOGL", "AAPL"}
	etfs := []string{"SPY"}

	tests := []struct {
		name        string
		keywords    string
		searcher    *mockSearcher
		wantSymbols []string
		wantErr     bool
	}{
		{
			name:        "empty keywords returns empty without calling searcher",
			keywords:    "",
			searcher:    &mockSearcher{},
			wantSymbols: []string{},
		},
		{
			name:        "symbol-only hit",
			keywords:    "GOOG",
			searcher:    &mockSearcher{symbolMatches: []domain.Match{goog}, stockSymbols: stocks, etfSymbols: etfs},
			wantSymbols: []string{"GOOGL"},
		},
		{
			name:        "name-only hit (google/alphabet case)",
			keywords:    "google",
			searcher:    &mockSearcher{nameMatches: []domain.Match{goog}, stockSymbols: stocks, etfSymbols: etfs},
			wantSymbols: []string{"GOOGL"},
		},
		{
			name:     "overlap deduped by symbol, symbol match ordered first",
			keywords: "alphabet",
			searcher: &mockSearcher{
				symbolMatches: []domain.Match{goog},
				nameMatches:   []domain.Match{goog, aapl},
				stockSymbols:  stocks,
				etfSymbols:    etfs,
			},
			wantSymbols: []string{"GOOGL", "AAPL"},
		},
		{
			name:     "ETF result is kept",
			keywords: "sp500",
			searcher: &mockSearcher{
				nameMatches:  []domain.Match{spy},
				stockSymbols: stocks,
				etfSymbols:   etfs,
			},
			wantSymbols: []string{"SPY"},
		},
		{
			name:     "non stock/etf result (index) is filtered out",
			keywords: "s&p",
			searcher: &mockSearcher{
				symbolMatches: []domain.Match{index, aapl},
				stockSymbols:  stocks,
				etfSymbols:    etfs,
			},
			wantSymbols: []string{"AAPL"},
		},
		{
			name:     "case-insensitive symbol membership",
			keywords: "goog",
			searcher: &mockSearcher{
				symbolMatches: []domain.Match{{Symbol: "googl", Name: "Alphabet Inc."}},
				stockSymbols:  stocks,
				etfSymbols:    etfs,
			},
			wantSymbols: []string{"googl"},
		},
		{
			name:     "symbol errors, name succeeds -> degrade gracefully",
			keywords: "google",
			searcher: &mockSearcher{
				symbolErr:    errors.New("symbol endpoint down"),
				nameMatches:  []domain.Match{goog},
				stockSymbols: stocks,
				etfSymbols:   etfs,
			},
			wantSymbols: []string{"GOOGL"},
		},
		{
			name:     "both search endpoints error -> error returned",
			keywords: "google",
			searcher: &mockSearcher{
				symbolErr: errors.New("symbol endpoint down"),
				nameErr:   errors.New("name endpoint down"),
			},
			wantErr: true,
		},
		{
			name:     "universe fetch errors -> results returned unfiltered",
			keywords: "s&p",
			searcher: &mockSearcher{
				symbolMatches: []domain.Match{index, aapl},
				listErr:       errors.New("list endpoint down"),
			},
			wantSymbols: []string{"^GSPC", "AAPL"},
		},
		{
			name:     "empty universe -> results returned unfiltered (fail-safe)",
			keywords: "s&p",
			searcher: &mockSearcher{
				symbolMatches: []domain.Match{index, aapl},
				stockSymbols:  []string{},
				etfSymbols:    []string{},
			},
			wantSymbols: []string{"^GSPC", "AAPL"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := stockuc.NewSearchUseCase(tc.searcher, tc.searcher, time.Minute)
			got, err := uc.Execute(tc.keywords)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.wantSymbols) {
				t.Fatalf("expected %d matches %v, got %d: %+v", len(tc.wantSymbols), tc.wantSymbols, len(got), got)
			}
			for i, sym := range tc.wantSymbols {
				if got[i].Symbol != sym {
					t.Errorf("match[%d]: expected symbol %s, got %s", i, sym, got[i].Symbol)
				}
			}
		})
	}
}
