package dca

import (
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
)

// holdingsConcurrency bounds how many price histories are loaded at once, so a portfolio at
// its position limit does not open dozens of provider requests together.
const holdingsConcurrency = 4

// CombinedSymbol is the symbol on the combined result of a holdings simulation.
const CombinedSymbol = "PORTFOLIO"

// SimulateHoldings runs the plan across a portfolio's holdings at fixed weights: every
// purchase of plan.Amount is split between the assets in proportion to weight. It is a
// hypothetical, forward-weighted replay, not the user's real purchases.
//
// An asset with no usable history for the range is skipped and its weight is shared out
// among the rest, so the whole plan amount is still invested. Skipped assets are listed with
// a reason. It returns an error only when nothing could be simulated and at least one asset
// failed for a reason other than missing history (a provider outage).
func (uc *SimulatorUseCase) SimulateHoldings(holdings []dca.Holding, plan dca.SimulationInput) (*dca.PortfolioSimulation, error) {
	var totalWeight float64
	for _, h := range holdings {
		if h.Weight > 0 {
			totalWeight += h.Weight
		}
	}
	if totalWeight <= 0 {
		return nil, errors.New("holdings need a positive total weight")
	}

	type outcome struct {
		share  float64 // weight / totalWeight, before skipped assets are dropped
		result *dca.SimulationResult
		err    error
	}
	outcomes := make([]outcome, len(holdings))
	sem := make(chan struct{}, holdingsConcurrency)
	var wg sync.WaitGroup
	for i, h := range holdings {
		outcomes[i].share = h.Weight / totalWeight
		if h.Weight <= 0 {
			outcomes[i].err = dca.ErrSymbolNotFound
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			in := plan
			in.Symbol = h.Symbol
			in.Amount = plan.Amount * outcomes[i].share
			outcomes[i].result, outcomes[i].err = uc.Execute(in)
		}()
	}
	wg.Wait()

	sim := &dca.PortfolioSimulation{Assets: []dca.PortfolioAsset{}, Skipped: []dca.SkippedSymbol{}}
	var upstream error
	var kept float64
	var survivors []int
	for i, o := range outcomes {
		switch {
		case o.err == nil:
			survivors = append(survivors, i)
			kept += o.share
		case errors.Is(o.err, dca.ErrSymbolNotFound):
			sim.Skipped = append(sim.Skipped, dca.SkippedSymbol{Symbol: holdings[i].Symbol, Reason: "No price history for this date range"})
		case errors.Is(o.err, dca.ErrDateRangeTooShort):
			sim.Skipped = append(sim.Skipped, dca.SkippedSymbol{Symbol: holdings[i].Symbol, Reason: "Not enough price history for this plan"})
		default:
			upstream = o.err
			sim.Skipped = append(sim.Skipped, dca.SkippedSymbol{Symbol: holdings[i].Symbol, Reason: "Price history could not be loaded"})
		}
	}
	if len(survivors) == 0 {
		if upstream != nil {
			return nil, upstream
		}
		return sim, nil
	}

	// Simulation is linear in the amount, so sharing a skipped asset's weight among the rest is
	// a rescale of the survivors' results by 1/kept; percentages are unaffected.
	scale := 1 / kept
	for _, i := range survivors {
		o := outcomes[i]
		scaleResult(o.result, scale)
		sim.Assets = append(sim.Assets, dca.PortfolioAsset{
			Symbol:    holdings[i].Symbol,
			WeightPct: o.share * scale * 100,
			Result:    o.result,
		})
	}
	sim.Combined = combineAssets(plan, sim.Assets)
	return sim, nil
}

// scaleResult multiplies every money and unit amount in r by k. Percentages stay as they are.
func scaleResult(r *dca.SimulationResult, k float64) {
	r.AmountPerPeriod *= k
	r.TotalInvested *= k
	r.FinalPortfolioValue *= k
	r.TotalReturn *= k
	r.TotalUnits *= k
	for i := range r.DataPoints {
		dp := &r.DataPoints[i]
		dp.UnitsPurchased *= k
		dp.TotalUnits *= k
		dp.TotalInvested *= k
		dp.PortfolioValue *= k
	}
}

// combineAssets adds the assets' (already scaled) results into one portfolio result. At each
// date an asset counts as its latest data point on or before that date, so assets with
// slightly different trading calendars still line up.
func combineAssets(plan dca.SimulationInput, assets []dca.PortfolioAsset) *dca.SimulationResult {
	dateSet := map[string]bool{}
	for _, a := range assets {
		for _, dp := range a.Result.DataPoints {
			dateSet[dp.Date] = true
		}
	}
	dates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	next := make([]int, len(assets)) // index of the first data point after the current date
	points := make([]dca.DataPoint, 0, len(dates))
	for _, d := range dates {
		var invested, value float64
		for i, a := range assets {
			dps := a.Result.DataPoints
			for next[i] < len(dps) && dps[next[i]].Date <= d {
				next[i]++
			}
			if next[i] > 0 {
				invested += dps[next[i]-1].TotalInvested
				value += dps[next[i]-1].PortfolioValue
			}
		}
		ret := 0.0
		if invested > 0 {
			ret = (value - invested) / invested * 100
		}
		points = append(points, dca.DataPoint{Date: d, TotalInvested: invested, PortfolioValue: value, ReturnPct: ret})
	}

	var flows []cashFlow
	purchaseDates := map[string]bool{}
	var finalValue, invested float64
	for _, a := range assets {
		finalValue += a.Result.FinalPortfolioValue
		invested += a.Result.TotalInvested
		prev := 0.0
		for _, dp := range a.Result.DataPoints {
			if dp.UnitsPurchased > 0 {
				when, _ := parseDay(dp.Date)
				flows = append(flows, cashFlow{Date: when, Amount: dp.TotalInvested - prev})
				purchaseDates[dp.Date] = true
			}
			prev = dp.TotalInvested
		}
	}
	// Value everything as of the latest date any asset has a price.
	end := latestDate(assets)

	years := plan.EndDate.Sub(plan.StartDate).Hours() / (365.25 * 24)
	totalReturn := finalValue - invested
	return &dca.SimulationResult{
		Symbol:                  CombinedSymbol,
		StartDate:               plan.StartDate.Format("2006-01-02"),
		EndDate:                 plan.EndDate.Format("2006-01-02"),
		Frequency:               plan.Frequency,
		AmountPerPeriod:         plan.Amount,
		TotalInvested:           invested,
		FinalPortfolioValue:     finalValue,
		TotalReturn:             totalReturn,
		TotalReturnPct:          totalReturn / invested * 100,
		AnnualizedReturnPct:     (math.Pow(finalValue/invested, 1/years) - 1) * 100,
		AnnualizedReturnNote:    annualizedReturnNote,
		MoneyWeightedReturnPct:  moneyWeightedReturnPct(flows, end, finalValue),
		MoneyWeightedReturnNote: moneyWeightedReturnNote,
		PriceBasisNote:          priceBasisNote,
		PeriodsCount:            len(purchaseDates),
		DataPoints:              points,
	}
}

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

// latestDate is the last data point date across the assets.
func latestDate(assets []dca.PortfolioAsset) time.Time {
	var latest time.Time
	for _, a := range assets {
		if t, ok := parseDay(a.Result.DataPoints[len(a.Result.DataPoints)-1].Date); ok && t.After(latest) {
			latest = t
		}
	}
	return latest
}
