package dca_test

import (
	"errors"
	"math"
	"testing"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func holdingsFetcher() *perSymbolFetcher {
	start, end := date("2023-01-02"), date("2023-06-30")
	return &perSymbolFetcher{prices: map[string][]dca.HistoricalPrice{
		"FLAT": buildPrices(start, end, 100),
		"UP":   risingPrices(start, end, 50),
	}}
}

func TestSimulateHoldings_SplitsEachPurchaseByWeight(t *testing.T) {
	uc := dcauc.NewSimulatorUseCase(holdingsFetcher())
	sim, err := uc.SimulateHoldings([]dca.Holding{{Symbol: "FLAT", Weight: 60}, {Symbol: "UP", Weight: 40}}, comparePlan())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sim.Assets) != 2 || len(sim.Skipped) != 0 {
		t.Fatalf("assets %d, skipped %d", len(sim.Assets), len(sim.Skipped))
	}
	flat, up := sim.Assets[0], sim.Assets[1]
	if !near(flat.WeightPct, 60) || !near(up.WeightPct, 40) {
		t.Errorf("weights %f / %f, want 60 / 40", flat.WeightPct, up.WeightPct)
	}
	// 6 monthly purchases of $100: $60 into FLAT, $40 into UP each time.
	if !near(flat.Result.TotalInvested, 360) || !near(up.Result.TotalInvested, 240) {
		t.Errorf("invested %f / %f, want 360 / 240", flat.Result.TotalInvested, up.Result.TotalInvested)
	}
	c := sim.Combined
	if !near(c.TotalInvested, 600) || !near(c.AmountPerPeriod, 100) {
		t.Errorf("combined invested %f, per period %f", c.TotalInvested, c.AmountPerPeriod)
	}
	if !near(c.FinalPortfolioValue, flat.Result.FinalPortfolioValue+up.Result.FinalPortfolioValue) {
		t.Errorf("combined final %f is not the sum of the assets", c.FinalPortfolioValue)
	}
	if !near(c.TotalReturn, c.FinalPortfolioValue-c.TotalInvested) {
		t.Errorf("total return %f", c.TotalReturn)
	}
	if c.PeriodsCount != 6 {
		t.Errorf("periods = %d, want 6", c.PeriodsCount)
	}
	last := c.DataPoints[len(c.DataPoints)-1]
	if !near(last.PortfolioValue, c.FinalPortfolioValue) || !near(last.TotalInvested, c.TotalInvested) {
		t.Errorf("last point %+v does not match the totals", last)
	}
	if c.MoneyWeightedReturnPct == nil || *c.MoneyWeightedReturnPct <= 0 {
		t.Errorf("money-weighted return = %v, want a positive rate", c.MoneyWeightedReturnPct)
	}
}

func TestSimulateHoldings_AWeightOfOneAssetMatchesThatAssetAlone(t *testing.T) {
	uc := dcauc.NewSimulatorUseCase(holdingsFetcher())
	sim, err := uc.SimulateHoldings([]dca.Holding{{Symbol: "UP", Weight: 7}}, comparePlan())
	if err != nil {
		t.Fatal(err)
	}
	single, err := uc.Execute(dca.SimulationInput{
		Symbol: "UP", StartDate: comparePlan().StartDate, EndDate: comparePlan().EndDate, Amount: 100, Frequency: dca.FrequencyMonthly,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := sim.Combined
	if !near(c.TotalInvested, single.TotalInvested) || !near(c.FinalPortfolioValue, single.FinalPortfolioValue) ||
		!near(c.TotalReturnPct, single.TotalReturnPct) || !near(c.AnnualizedReturnPct, single.AnnualizedReturnPct) {
		t.Errorf("combined %+v differs from the single-asset result %+v", c, single)
	}
	if c.MoneyWeightedReturnPct == nil || single.MoneyWeightedReturnPct == nil ||
		math.Abs(*c.MoneyWeightedReturnPct-*single.MoneyWeightedReturnPct) > 1e-6 {
		t.Errorf("money-weighted %v vs %v", c.MoneyWeightedReturnPct, single.MoneyWeightedReturnPct)
	}
}

func TestSimulateHoldings_SharesASkippedAssetsWeightAmongTheRest(t *testing.T) {
	f := holdingsFetcher()
	f.errs = map[string]error{"GONE": dca.ErrSymbolNotFound}
	uc := dcauc.NewSimulatorUseCase(f)

	sim, err := uc.SimulateHoldings([]dca.Holding{
		{Symbol: "FLAT", Weight: 50}, {Symbol: "GONE", Weight: 25}, {Symbol: "UP", Weight: 25},
	}, comparePlan())
	if err != nil {
		t.Fatal(err)
	}
	if len(sim.Skipped) != 1 || sim.Skipped[0].Symbol != "GONE" || sim.Skipped[0].Reason == "" {
		t.Fatalf("skipped = %+v", sim.Skipped)
	}
	// 50 : 25 among the survivors becomes 2/3 : 1/3, and the whole $100 is still invested.
	if !near(sim.Assets[0].WeightPct, 200.0/3) || !near(sim.Assets[1].WeightPct, 100.0/3) {
		t.Errorf("weights %f / %f", sim.Assets[0].WeightPct, sim.Assets[1].WeightPct)
	}
	if !near(sim.Combined.TotalInvested, 600) {
		t.Errorf("combined invested %f, want the full 600", sim.Combined.TotalInvested)
	}
}

func TestSimulateHoldings_NothingToSimulateIsNotAnErrorUnlessTheProviderFailed(t *testing.T) {
	f := &perSymbolFetcher{errs: map[string]error{"A": dca.ErrSymbolNotFound}}
	sim, err := dcauc.NewSimulatorUseCase(f).SimulateHoldings([]dca.Holding{{Symbol: "A", Weight: 1}}, comparePlan())
	if err != nil || sim.Combined != nil || len(sim.Assets) != 0 || len(sim.Skipped) != 1 {
		t.Fatalf("no-history: %+v, %v", sim, err)
	}

	boom := errors.New("upstream down")
	f = &perSymbolFetcher{errs: map[string]error{"A": boom}}
	if _, err := dcauc.NewSimulatorUseCase(f).SimulateHoldings([]dca.Holding{{Symbol: "A", Weight: 1}}, comparePlan()); !errors.Is(err, boom) {
		t.Fatalf("want the upstream error, got %v", err)
	}
}

func TestSimulateHoldings_RejectsNoWeight(t *testing.T) {
	uc := dcauc.NewSimulatorUseCase(holdingsFetcher())
	if _, err := uc.SimulateHoldings([]dca.Holding{{Symbol: "FLAT", Weight: 0}}, comparePlan()); err == nil {
		t.Fatal("want an error for zero total weight")
	}
}

func TestSimulateHoldings_HandlesAFullPortfolioOfTwentyHoldings(t *testing.T) {
	start, end := date("2023-01-02"), date("2023-06-30")
	f := &perSymbolFetcher{prices: map[string][]dca.HistoricalPrice{}}
	var holdings []dca.Holding
	for i := 0; i < 20; i++ {
		sym := string(rune('A' + i))
		f.prices[sym] = buildPrices(start, end, float64(10+i))
		holdings = append(holdings, dca.Holding{Symbol: sym, Weight: 1})
	}
	sim, err := dcauc.NewSimulatorUseCase(f).SimulateHoldings(holdings, comparePlan())
	if err != nil || len(sim.Assets) != 20 || !near(sim.Combined.TotalInvested, 600) {
		t.Fatalf("got %d assets, invested %v, err %v", len(sim.Assets), sim.Combined, err)
	}
}
