package dca

import (
	"math"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
)

type SimulatorUseCase struct {
	fetcher dca.PriceFetcher
}

func NewSimulatorUseCase(fetcher dca.PriceFetcher) *SimulatorUseCase {
	return &SimulatorUseCase{fetcher: fetcher}
}

func (uc *SimulatorUseCase) Execute(input dca.SimulationInput) (*dca.SimulationResult, error) {
	prices, err := uc.fetcher.GetHistoricalPrices(input.Symbol, input.StartDate, input.EndDate)
	if err != nil {
		return nil, err
	}
	if len(prices) == 0 {
		return nil, dca.ErrSymbolNotFound
	}

	priceMap := make(map[string]float64, len(prices))
	for _, p := range prices {
		priceMap[p.Date.Format("2006-01-02")] = p.AdjClose
	}

	resolvedDates := resolveDates(input.StartDate, input.EndDate, input.Frequency, priceMap)
	if len(resolvedDates) < 2 {
		return nil, dca.ErrDateRangeTooShort
	}

	var totalUnits, totalInvested float64
	dataPoints := make([]dca.DataPoint, 0, len(resolvedDates))

	for _, dateKey := range resolvedDates {
		price := priceMap[dateKey]
		unitsPurchased := input.Amount / price
		totalUnits += unitsPurchased
		totalInvested += input.Amount
		portfolioValue := totalUnits * price
		returnPct := ((portfolioValue - totalInvested) / totalInvested) * 100

		dataPoints = append(dataPoints, dca.DataPoint{
			Date:           dateKey,
			Price:          price,
			UnitsPurchased: unitsPurchased,
			TotalUnits:     totalUnits,
			TotalInvested:  totalInvested,
			PortfolioValue: portfolioValue,
			ReturnPct:      returnPct,
		})
	}

	lastPrice := prices[len(prices)-1].AdjClose
	finalPortfolioValue := totalUnits * lastPrice
	totalReturn := finalPortfolioValue - totalInvested
	totalReturnPct := (totalReturn / totalInvested) * 100

	years := input.EndDate.Sub(input.StartDate).Hours() / (365.25 * 24)
	annualizedReturnPct := (math.Pow(finalPortfolioValue/totalInvested, 1/years) - 1) * 100

	return &dca.SimulationResult{
		Symbol:               input.Symbol,
		StartDate:            input.StartDate.Format("2006-01-02"),
		EndDate:              input.EndDate.Format("2006-01-02"),
		Frequency:            input.Frequency,
		AmountPerPeriod:      input.Amount,
		TotalInvested:        totalInvested,
		FinalPortfolioValue:  finalPortfolioValue,
		TotalReturn:          totalReturn,
		TotalReturnPct:       totalReturnPct,
		AnnualizedReturnPct:  annualizedReturnPct,
		AnnualizedReturnNote: "CAGR-based estimate (total-capital basis, not IRR)",
		PeriodsCount:         len(resolvedDates),
		TotalUnits:           totalUnits,
		DataPoints:           dataPoints,
	}, nil
}

func resolveDates(start, end time.Time, freq dca.Frequency, priceMap map[string]float64) []string {
	if freq == dca.FrequencyDaily {
		var dates []string
		for cur := start; !cur.After(end); cur = cur.AddDate(0, 0, 1) {
			key := cur.Format("2006-01-02")
			if _, ok := priceMap[key]; ok {
				dates = append(dates, key)
			}
		}
		return dates
	}

	targets := targetDates(start, end, freq)
	resolved := make([]string, 0, len(targets))
	seen := make(map[string]bool)
	for _, t := range targets {
		if date, _, found := nextTradingDay(t, priceMap); found {
			if !seen[date] {
				seen[date] = true
				resolved = append(resolved, date)
			}
		}
	}
	return resolved
}

func targetDates(start, end time.Time, freq dca.Frequency) []time.Time {
	var targets []time.Time

	switch freq {
	case dca.FrequencyWeekly:
		// Monday of each calendar week
		cur := mondayOf(start)
		for !cur.After(end) {
			targets = append(targets, cur)
			cur = cur.AddDate(0, 0, 7)
		}

	case dca.FrequencyBiweekly:
		// Monday of every other calendar week, anchored to week containing startDate
		cur := mondayOf(start)
		for !cur.After(end) {
			targets = append(targets, cur)
			cur = cur.AddDate(0, 0, 14)
		}

	case dca.FrequencyMonthly:
		// 1st of each calendar month
		cur := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
		for !cur.After(end) {
			targets = append(targets, cur)
			cur = cur.AddDate(0, 1, 0)
		}
	}

	return targets
}

func mondayOf(t time.Time) time.Time {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	daysBack := weekday - 1
	return t.AddDate(0, 0, -daysBack)
}

func nextTradingDay(target time.Time, priceMap map[string]float64) (date string, price float64, found bool) {
	for i := 0; i < 7; i++ {
		key := target.AddDate(0, 0, i).Format("2006-01-02")
		if p, ok := priceMap[key]; ok {
			return key, p, true
		}
	}
	return "", 0, false
}
