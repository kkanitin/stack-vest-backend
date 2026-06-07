package stockuc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
)

type mockHistoryFetcher struct {
	bySymbol map[string][]domain.HistoryPoint
	err      error
}

func (m *mockHistoryFetcher) GetHistoryClose(symbol string, _, _ time.Time) ([]domain.HistoryPoint, error) {
	if m.err != nil {
		return nil, m.err
	}
	pts, ok := m.bySymbol[symbol]
	if !ok {
		return nil, domain.ErrSymbolNotFound
	}
	return pts, nil
}

func TestBatchHistoryUseCase_Execute(t *testing.T) {
	tests := []struct {
		name        string
		symbolsParm string
		rangeParam  domain.BatchHistoryRange
		bySymbol    map[string][]domain.HistoryPoint
		fetchErr    error
		wantItems   []domain.BatchHistoryItem
		wantErr     error
	}{
		{
			name:        "success preserves order",
			symbolsParm: "AAPL,MSFT",
			rangeParam:  domain.BatchRange7D,
			bySymbol: map[string][]domain.HistoryPoint{
				"AAPL": {{Date: "2026-06-01", Close: 100}},
				"MSFT": {{Date: "2026-06-01", Close: 200}},
			},
			wantItems: []domain.BatchHistoryItem{
				{Symbol: "AAPL", Range: "7D", Points: []domain.HistoryPoint{{Date: "2026-06-01", Close: 100}}},
				{Symbol: "MSFT", Range: "7D", Points: []domain.HistoryPoint{{Date: "2026-06-01", Close: 200}}},
			},
		},
		{
			name:        "missing symbol returns empty points not error",
			symbolsParm: "AAPL,ZZZZ",
			rangeParam:  domain.BatchRange30D,
			bySymbol: map[string][]domain.HistoryPoint{
				"AAPL": {{Date: "2026-06-01", Close: 100}},
			},
			wantItems: []domain.BatchHistoryItem{
				{Symbol: "AAPL", Range: "30D", Points: []domain.HistoryPoint{{Date: "2026-06-01", Close: 100}}},
				{Symbol: "ZZZZ", Range: "30D", Points: []domain.HistoryPoint{}},
			},
		},
		{
			name:        "too many symbols",
			symbolsParm: "A,B,C,D,E,F,G,H,I,J,K",
			rangeParam:  domain.BatchRange7D,
			wantErr:     domain.ErrTooManySymbols,
		},
		{
			name:        "transport error fails whole request",
			symbolsParm: "AAPL,MSFT",
			rangeParam:  domain.BatchRange7D,
			fetchErr:    errors.New("upstream error"),
			wantErr:     errors.New("upstream error"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := stockuc.NewBatchHistoryUseCase(&mockHistoryFetcher{bySymbol: tc.bySymbol, err: tc.fetchErr})
			result, err := uc.Execute(context.Background(), tc.symbolsParm, tc.rangeParam)

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
			if len(result) != len(tc.wantItems) {
				t.Fatalf("expected %d items, got %d", len(tc.wantItems), len(result))
			}
			for i, want := range tc.wantItems {
				got := result[i]
				if got.Symbol != want.Symbol || got.Range != want.Range {
					t.Errorf("item[%d]: got %+v, want %+v", i, got, want)
				}
				if len(got.Points) != len(want.Points) {
					t.Errorf("item[%d].Points: got %d points, want %d", i, len(got.Points), len(want.Points))
				}
			}
		})
	}
}
