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
		prices = append(prices, dca.HistoricalPrice{Date: cur, Close: price})
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
			prices = append(prices, dca.HistoricalPrice{Date: cur, Close: p})
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

// TestMonthlyMidMonthStart guards against a regression where monthly targets were
// hard-anchored to the 1st of the calendar month regardless of startDate. For a
// mid-month start (the 15th), the old code's first target (Jan 1) predated
// startDate entirely, fell outside the fetched price range, and was silently
// dropped — yielding PeriodsCount=2 instead of 3 for this Jan-Mar range.
func TestMonthlyMidMonthStart(t *testing.T) {
	start := date("2023-01-15")
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
		if dp.UnitsPurchased == 0 {
			continue // closing point, not a purchase
		}
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
			prices: []dca.HistoricalPrice{{Date: date("2023-01-02"), Close: 100.0}},
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

// risingPrices returns weekday closes that grow 0.1% per calendar day from base.
func risingPrices(start, end time.Time, base float64) []dca.HistoricalPrice {
	var prices []dca.HistoricalPrice
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
		if cur.Weekday() == time.Saturday || cur.Weekday() == time.Sunday {
			continue
		}
		days := cur.Sub(start).Hours() / 24
		prices = append(prices, dca.HistoricalPrice{Date: cur, Close: base * math.Pow(1.001, days)})
	}
	return prices
}

func TestMoneyWeightedReturn_SolvesIRROfTheDatedPurchases(t *testing.T) {
	start, end := date("2022-01-03"), date("2024-01-03")
	prices := risingPrices(start, end, 100)

	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: prices})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol: "TEST", StartDate: start, EndDate: end, Amount: 100, Frequency: dca.FrequencyMonthly,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MoneyWeightedReturnPct == nil {
		t.Fatal("moneyWeightedReturnPct is nil")
	}
	r := *result.MoneyWeightedReturnPct / 100

	// Independent check: discounting every dated cash flow at the returned rate nets to ~0.
	endDate := date(result.EndDate)
	npv := result.FinalPortfolioValue
	for _, dp := range result.DataPoints {
		if dp.UnitsPurchased == 0 {
			continue // closing point, not a purchase
		}
		years := endDate.Sub(date(dp.Date)).Hours() / (365 * 24)
		npv -= result.AmountPerPeriod * math.Pow(1+r, years)
	}
	if math.Abs(npv) > 1e-4 {
		t.Errorf("NPV at the returned rate = %f, want ~0 (rate %f)", npv, r)
	}

	// Steady growth: money invested later has had less time to grow, so the dated rate beats
	// the total-capital CAGR, which treats every purchase as made on day one.
	if *result.MoneyWeightedReturnPct <= result.AnnualizedReturnPct {
		t.Errorf("money-weighted %f should exceed CAGR %f for a steadily rising price",
			*result.MoneyWeightedReturnPct, result.AnnualizedReturnPct)
	}
	if result.MoneyWeightedReturnNote == "" || result.PriceBasisNote == "" {
		t.Error("notes must be set")
	}
}

func TestMoneyWeightedReturn_FlatPriceIsZero(t *testing.T) {
	start, end := date("2023-01-02"), date("2023-12-29")
	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: buildPrices(start, end, 100)})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol: "TEST", StartDate: start, EndDate: end, Amount: 100, Frequency: dca.FrequencyWeekly,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MoneyWeightedReturnPct == nil || math.Abs(*result.MoneyWeightedReturnPct) > 1e-4 {
		t.Errorf("flat price should give ~0, got %v", result.MoneyWeightedReturnPct)
	}
}

func TestDataPoints_EndOnTheLastTradingDayWithTheFinalValue(t *testing.T) {
	start, end := date("2023-01-02"), date("2023-03-31")
	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: risingPrices(start, end, 100)})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol: "TEST", StartDate: start, EndDate: end, Amount: 100, Frequency: dca.FrequencyMonthly,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	last := result.DataPoints[len(result.DataPoints)-1]
	if last.Date != "2023-03-31" {
		t.Errorf("last data point date = %s, want the last trading day 2023-03-31", last.Date)
	}
	if last.PortfolioValue != result.FinalPortfolioValue {
		t.Errorf("last portfolioValue %f != finalPortfolioValue %f", last.PortfolioValue, result.FinalPortfolioValue)
	}
	if last.UnitsPurchased != 0 || last.TotalInvested != result.TotalInvested {
		t.Errorf("closing point must not add a purchase: %+v", last)
	}
	if result.PeriodsCount != 3 {
		t.Errorf("periodsCount = %d, want 3 purchases (closing point excluded)", result.PeriodsCount)
	}
}

func TestDataPoints_NoExtraPointWhenLastPurchaseIsTheLastDay(t *testing.T) {
	// Daily purchases: the last trading day is itself a purchase.
	start, end := date("2023-01-02"), date("2023-01-13")
	uc := dcauc.NewSimulatorUseCase(&mockFetcher{prices: buildPrices(start, end, 100)})
	result, err := uc.Execute(dca.SimulationInput{
		Symbol: "TEST", StartDate: start, EndDate: end, Amount: 100, Frequency: dca.FrequencyDaily,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.DataPoints) != result.PeriodsCount {
		t.Errorf("got %d data points for %d purchases", len(result.DataPoints), result.PeriodsCount)
	}
}
