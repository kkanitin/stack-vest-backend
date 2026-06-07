package stockuc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
)

type mockDetailFetcher struct {
	profile  *domain.AssetProfile
	daily    []domain.DetailPoint
	intraday []domain.DetailPoint
	err      error
}

func (m *mockDetailFetcher) GetProfile(_ string) (*domain.AssetProfile, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.profile, nil
}

func (m *mockDetailFetcher) GetDailyOHLCV(_ string, _, _ time.Time) ([]domain.DetailPoint, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.daily, nil
}

func (m *mockDetailFetcher) GetIntradayOHLCV(_ string) ([]domain.DetailPoint, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.intraday, nil
}

func TestDetailUseCase_Execute(t *testing.T) {
	tests := []struct {
		name         string
		rangeParam   domain.DetailRange
		fetcher      *mockDetailFetcher
		wantInterval string
		wantPoints   int
		wantErr      bool
	}{
		{
			name:       "intraday range uses 5-min OHLCV",
			rangeParam: domain.DetailRange1D,
			fetcher: &mockDetailFetcher{
				profile:  &domain.AssetProfile{Name: "Apple Inc.", Currency: "USD"},
				intraday: []domain.DetailPoint{{Date: "2026-06-01 09:30:00", Close: 100}},
				daily:    []domain.DetailPoint{{Date: "2026-06-01", Close: 999}},
			},
			wantInterval: "intraday",
			wantPoints:   1,
		},
		{
			name:       "daily range uses daily OHLCV",
			rangeParam: domain.DetailRange1M,
			fetcher: &mockDetailFetcher{
				profile: &domain.AssetProfile{Name: "Apple Inc.", Currency: "USD"},
				daily:   []domain.DetailPoint{{Date: "2026-06-01", Close: 100}, {Date: "2026-06-02", Close: 101}},
			},
			wantInterval: "daily",
			wantPoints:   2,
		},
		{
			name:       "fetcher error fails the request",
			rangeParam: domain.DetailRange1D,
			fetcher:    &mockDetailFetcher{err: errors.New("upstream error")},
			wantErr:    true,
		},
		{
			name:       "symbol not found",
			rangeParam: domain.DetailRange1D,
			fetcher:    &mockDetailFetcher{err: domain.ErrSymbolNotFound},
			wantErr:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := stockuc.NewDetailUseCase(tc.fetcher)
			result, err := uc.Execute(context.Background(), "AAPL", tc.rangeParam)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Interval != tc.wantInterval {
				t.Errorf("Interval: got %q, want %q", result.Interval, tc.wantInterval)
			}
			if len(result.Points) != tc.wantPoints {
				t.Errorf("Points: got %d, want %d", len(result.Points), tc.wantPoints)
			}
			if result.Name != "Apple Inc." || result.Currency != "USD" {
				t.Errorf("profile data not merged: got Name=%q Currency=%q", result.Name, result.Currency)
			}
			if result.Symbol != "AAPL" || result.Range != tc.rangeParam {
				t.Errorf("Symbol/Range not set correctly: got %+v", result)
			}
		})
	}
}
