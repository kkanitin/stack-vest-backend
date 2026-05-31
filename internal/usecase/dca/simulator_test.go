package dca_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
)

type mockFetcher struct {
	prices []dca.HistoricalPrice
	err    error
}

func (m *mockFetcher) GetHistoricalPrices(_ string, _, _ time.Time) ([]dca.HistoricalPrice, error) {
	return m.prices, m.err
}

func date(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

// buildPrices generates a slice of trading-day prices from start to end at a fixed price,
// skipping weekends.
func buildPrices(start, end time.Time, price float64) []dca.HistoricalPrice {
	var prices []dca.HistoricalPrice
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		if cur.Weekday() == time.Saturday || cur.Weekday() == time.Sunday {
			continue
		}
		prices = append(prices, dca.HistoricalPrice{Date: cur, AdjClose: price})
	}
	return prices
}

func TestMonthlyFlatPrice(t *testing.T) {
	start := date("2023-01-01")
	end := date("2023-03-31")
	prices := buildPrices(start, end, 100.0)

	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: prices})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol:    "TEST",
		StartDate: start,
		EndDate:   end,
		Amount:    100.0,
		Frequency: dca.FrequencyMonthly,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PeriodsCount != 3 {
		t.Errorf("periodsCount: got %d, want 3", result.PeriodsCount)
	}
	if result.TotalInvested != 300.0 {
		t.Errorf("totalInvested: got %f, want 300", result.TotalInvested)
	}
	if math.Abs(result.TotalUnits-3.0) > 1e-9 {
		t.Errorf("totalUnits: got %f, want 3.0", result.TotalUnits)
	}
	// Flat price → no return
	last := result.DataPoints[len(result.DataPoints)-1]
	if math.Abs(last.ReturnPct) > 1e-6 {
		t.Errorf("returnPct should be ~0 for flat price, got %f", last.ReturnPct)
	}
}

func TestMonthlyRisingPrice(t *testing.T) {
	// Jan at $100, Feb at $110, Mar at $120
	var prices []dca.HistoricalPrice
	for m := 1; m <= 3; m++ {
		p := 100.0 + float64(m-1)*10
		start := time.Date(2023, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(2023, time.Month(m)+1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
			if cur.Weekday() == time.Saturday || cur.Weekday() == time.Sunday {
				continue
			}
			prices = append(prices, dca.HistoricalPrice{Date: cur, AdjClose: p})
		}
	}

	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: prices})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol:    "TEST",
		StartDate: date("2023-01-01"),
		EndDate:   date("2023-03-31"),
		Amount:    100.0,
		Frequency: dca.FrequencyMonthly,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TotalReturnPct <= 0 {
		t.Errorf("totalReturnPct should be > 0, got %f", result.TotalReturnPct)
	}
	if result.FinalPortfolioValue <= result.TotalInvested {
		t.Errorf("finalPortfolioValue should exceed totalInvested")
	}
}

func TestWeekendInvestmentDateResolvesToMonday(t *testing.T) {
	// 2023-01-01 is a Sunday; the mock only has data from Mon 2023-01-02 onwards
	start := date("2023-01-01")
	end := date("2023-03-31")
	prices := buildPrices(date("2023-01-02"), end, 50.0)

	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: prices})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol:    "TEST",
		StartDate: start,
		EndDate:   end,
		Amount:    50.0,
		Frequency: dca.FrequencyMonthly,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First data point should resolve to 2023-01-02 (Monday after Sunday Jan 1)
	if result.DataPoints[0].Date != "2023-01-02" {
		t.Errorf("first date: got %s, want 2023-01-02", result.DataPoints[0].Date)
	}
}

func TestBiweeklyAnchoring(t *testing.T) {
	// 8 weeks of daily prices starting on a Monday (2023-01-02)
	start := date("2023-01-02")
	end := date("2023-02-26")
	prices := buildPrices(start, end, 100.0)

	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: prices})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol:    "TEST",
		StartDate: start,
		EndDate:   end,
		Amount:    100.0,
		Frequency: dca.FrequencyBiweekly,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 8-week range biweekly → should be exactly 4 periods
	if result.PeriodsCount != 4 {
		t.Errorf("periodsCount: got %d, want 4", result.PeriodsCount)
	}
	// Every investment date should be a Monday
	for _, dp := range result.DataPoints {
		d := date(dp.Date)
		if d.Weekday() != time.Monday {
			t.Errorf("expected Monday, got %s (%s)", d.Weekday(), dp.Date)
		}
	}
}

func TestSimulate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		prices  []dca.HistoricalPrice
		input   dca.SimulationInput
		wantErr error
	}{
		{
			name:   "empty prices → symbol not found",
			prices: nil,
			input: dca.SimulationInput{
				Symbol: "XYZZ", StartDate: date("2023-01-01"), EndDate: date("2023-12-31"),
				Amount: 100.0, Frequency: dca.FrequencyMonthly,
			},
			wantErr: dca.ErrSymbolNotFound,
		},
		{
			name:   "single period → date range too short",
			prices: []dca.HistoricalPrice{{Date: date("2023-01-02"), AdjClose: 100.0}},
			input: dca.SimulationInput{
				Symbol: "TEST", StartDate: date("2023-01-01"), EndDate: date("2023-01-31"),
				Amount: 100.0, Frequency: dca.FrequencyMonthly,
			},
			wantErr: dca.ErrDateRangeTooShort,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: tc.prices})
			_, err := uc.Execute(tc.input)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestCAGRFormula(t *testing.T) {
	// 2 years, flat $100, $100/month → 24 months
	start := date("2021-01-01")
	end := date("2022-12-31")
	prices := buildPrices(start, end, 100.0)

	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: prices})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol:    "TEST",
		StartDate: start,
		EndDate:   end,
		Amount:    100.0,
		Frequency: dca.FrequencyMonthly,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Flat price → finalPortfolioValue == totalInvested → CAGR = 0%
	if math.Abs(result.AnnualizedReturnPct) > 1e-6 {
		t.Errorf("annualizedReturnPct should be ~0 for flat price, got %f", result.AnnualizedReturnPct)
	}
}
