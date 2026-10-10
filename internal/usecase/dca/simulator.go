package dca

import (
	"errors"
	"math"
	"sync"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
)

// Notes sent with every result so a client can explain the figures it shows.
const (
	annualizedReturnNote    = "CAGR-based estimate (total-capital basis, not IRR)"
	moneyWeightedReturnNote = "Annualised IRR (XIRR): each purchase counts from its own date"
	priceBasisNote          = "Price movement only: closes are split-adjusted, dividends are not reinvested"
)

type SimulatorUseCase struct {
	fetcher dca.PriceFetcher
}

func NewSimulatorUseCase(fetcher dca.PriceFetcher) *SimulatorUseCase {
	return &SimulatorUseCase{fetcher: fetcher}
}

// Compare runs the same plan (amount, frequency, range) for each symbol. An asset with no
// usable history for the range is skipped, not an error. It returns an error only when
// nothing could be simulated and at least one asset failed for a reason other than missing
// history (a provider outage), so an outage is not reported as "no history".
func (uc *SimulatorUseCase) Compare(symbols []string, plan dca.SimulationInput) (*dca.Comparison, error) {
	type outcome struct {
		result *dca.SimulationResult
		err    error
	}
	outcomes := make([]outcome, len(symbols))

	var wg sync.WaitGroup
	for i, symbol := range symbols {
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := plan
			in.Symbol = symbol
			outcomes[i].result, outcomes[i].err = uc.Execute(in)
		}()
	}
	wg.Wait()

	cmp := &dca.Comparison{Results: []*dca.SimulationResult{}, Skipped: []dca.SkippedSymbol{}}
	var upstream error
	for i, o := range outcomes {
		switch {
		case o.err == nil:
			cmp.Results = append(cmp.Results, o.result)
		case errors.Is(o.err, dca.ErrSymbolNotFound):
			cmp.Skipped = append(cmp.Skipped, dca.SkippedSymbol{Symbol: symbols[i], Reason: "No price history for this date range"})
		case errors.Is(o.err, dca.ErrDateRangeTooShort):
			cmp.Skipped = append(cmp.Skipped, dca.SkippedSymbol{Symbol: symbols[i], Reason: "Not enough price history for this plan"})
		default:
			upstream = o.err
			cmp.Skipped = append(cmp.Skipped, dca.SkippedSymbol{Symbol: symbols[i], Reason: "Price history could not be loaded"})
		}
	}
	if len(cmp.Results) == 0 && upstream != nil {
		return nil, upstream
	}
	return cmp, nil
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
		priceMap[p.Date.Format("2006-01-02")] = p.Close
	}

	resolvedDates := resolveDates(input.StartDate, input.EndDate, input.Frequency, priceMap)
	if len(resolvedDates) < 2 {
		return nil, dca.ErrDateRangeTooShort
	}

	var totalUnits, totalInvested float64
	flows := make([]cashFlow, 0, len(resolvedDates))
	dataPoints := make([]dca.DataPoint, 0, len(resolvedDates))

	for _, dateKey := range resolvedDates {
		price := priceMap[dateKey]
		bought, _ := time.Parse("2006-01-02", dateKey)
		flows = append(flows, cashFlow{Date: bought, Amount: input.Amount})
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

	lastPrice := prices[len(prices)-1].Close
	lastDate := prices[len(prices)-1].Date
	finalPortfolioValue := totalUnits * lastPrice
	totalReturn := finalPortfolioValue - totalInvested
	totalReturnPct := (totalReturn / totalInvested) * 100

	// The last purchase is usually before the last trading day in the range. Close the series
	// there (no purchase) so its final point equals FinalPortfolioValue.
	if lastKey := lastDate.Format("2006-01-02"); lastKey != dataPoints[len(dataPoints)-1].Date {
		dataPoints = append(dataPoints, dca.DataPoint{
			Date:           lastKey,
			Price:          lastPrice,
			TotalUnits:     totalUnits,
			TotalInvested:  totalInvested,
			PortfolioValue: finalPortfolioValue,
			ReturnPct:      totalReturnPct,
		})
	}

	years := input.EndDate.Sub(input.StartDate).Hours() / (365.25 * 24)
	annualizedReturnPct := (math.Pow(finalPortfolioValue/totalInvested, 1/years) - 1) * 100

	moneyWeighted := moneyWeightedReturnPct(flows, lastDate, finalPortfolioValue)

	return &dca.SimulationResult{
		Symbol:                  input.Symbol,
		StartDate:               input.StartDate.Format("2006-01-02"),
		EndDate:                 input.EndDate.Format("2006-01-02"),
		Frequency:               input.Frequency,
		AmountPerPeriod:         input.Amount,
		TotalInvested:           totalInvested,
		FinalPortfolioValue:     finalPortfolioValue,
		TotalReturn:             totalReturn,
		TotalReturnPct:          totalReturnPct,
		AnnualizedReturnPct:     annualizedReturnPct,
		AnnualizedReturnNote:    annualizedReturnNote,
		MoneyWeightedReturnPct:  moneyWeighted,
		MoneyWeightedReturnNote: moneyWeightedReturnNote,
		PriceBasisNote:          priceBasisNote,
		PeriodsCount:            len(resolvedDates),
		TotalUnits:              totalUnits,
		DataPoints:              dataPoints,
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
		// Same day-of-month as startDate, then each subsequent calendar month —
		// mirrors weekly/biweekly's real-start-date anchoring instead of hard-anchoring
		// to the 1st of the calendar month (which could predate startDate and fall
		// outside the fetched price range).
		cur := start
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

// cashFlow is money put in on a date.
type cashFlow struct {
	Date   time.Time
	Amount float64
}

// moneyWeightedReturnPct is the annualised internal rate of return (XIRR) of putting in each
// flow on its date and holding until end, when the holding is worth finalValue. It returns
// nil when no rate can be solved (no flow before end, or a total loss).
//
// Multiplying NPV by (1+r)^T gives g(r) = finalValue - sum(amount*(1+r)^(T-t_i)), which is
// strictly decreasing in r when some flow predates end, so bisection finds the one root.
func moneyWeightedReturnPct(flows []cashFlow, end time.Time, finalValue float64) *float64 {
	const hoursPerYear = 365.0 * 24
	spans := make([]float64, len(flows))
	for i, f := range flows {
		spans[i] = end.Sub(f.Date).Hours() / hoursPerYear
	}
	g := func(r float64) float64 {
		sum := finalValue
		for i, years := range spans {
			sum -= flows[i].Amount * math.Pow(1+r, years)
		}
		return sum
	}

	lo, hi := -0.999999, 1.0
	if finalValue <= 0 || g(lo) <= 0 {
		return nil
	}
	for g(hi) > 0 {
		hi *= 2
		if hi > 1e9 {
			return nil
		}
	}
	for i := 0; i < 200; i++ {
		mid := (lo + hi) / 2
		if g(mid) > 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	pct := (lo + hi) / 2 * 100
	return &pct
}
