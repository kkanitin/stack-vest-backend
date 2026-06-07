package stockuc_test

import (
	"context"
	"errors"
	"testing"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
)

type mockPriceChanger struct {
	bySymbol map[string]*domain.PriceChange
	err      error
}

func (m *mockPriceChanger) GetPriceChange(symbol string) (*domain.PriceChange, error) {
	if m.err != nil {
		return nil, m.err
	}
	pc, ok := m.bySymbol[symbol]
	if !ok {
		return nil, domain.ErrSymbolNotFound
	}
	return pc, nil
}

func TestBatchPriceChangeUseCase_Execute(t *testing.T) {
	tests := []struct {
		name        string
		symbolsParm string
		bySymbol    map[string]*domain.PriceChange
		fetchErr    error
		wantSymbols []string
		wantErr     error
	}{
		{
			name:        "success preserves order and dedupes",
			symbolsParm: "AAPL, msft ,AAPL",
			bySymbol: map[string]*domain.PriceChange{
				"AAPL": {Symbol: "AAPL", D1: 1},
				"MSFT": {Symbol: "MSFT", D1: 2},
			},
			wantSymbols: []string{"AAPL", "MSFT"},
		},
		{
			name:        "missing symbol silently omitted",
			symbolsParm: "AAPL,ZZZZ",
			bySymbol: map[string]*domain.PriceChange{
				"AAPL": {Symbol: "AAPL", D1: 1},
			},
			wantSymbols: []string{"AAPL"},
		},
		{
			name:        "too many symbols",
			symbolsParm: "A,B,C,D,E,F,G,H,I,J,K",
			wantErr:     domain.ErrTooManySymbols,
		},
		{
			name:        "transport error fails whole request",
			symbolsParm: "AAPL,MSFT",
			fetchErr:    errors.New("upstream error"),
			wantErr:     errors.New("upstream error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := stockuc.NewBatchPriceChangeUseCase(&mockPriceChanger{bySymbol: tc.bySymbol, err: tc.fetchErr})
			result, err := uc.Execute(context.Background(), tc.symbolsParm)

			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if errors.Is(tc.wantErr, domain.ErrTooManySymbols) && !errors.Is(err, domain.ErrTooManySymbols) {
					t.Fatalf("expected ErrTooManySymbols, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != len(tc.wantSymbols) {
				t.Fatalf("expected %d results, got %d", len(tc.wantSymbols), len(result))
			}
			for i, sym := range tc.wantSymbols {
				if result[i].Symbol != sym {
					t.Errorf("result[%d].Symbol: got %q, want %q", i, result[i].Symbol, sym)
				}
			}
		})
	}
}
