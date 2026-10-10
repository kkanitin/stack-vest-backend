package dca_test

import (
	"errors"
	"testing"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
)

// perSymbolFetcher serves a different series, or error, per symbol.
type perSymbolFetcher struct {
	prices map[string][]dca.HistoricalPrice
	errs   map[string]error
}

func (f *perSymbolFetcher) GetHistoricalPrices(symbol string, _, _ time.Time) ([]dca.HistoricalPrice, error) {
	if err := f.errs[symbol]; err != nil {
		return nil, err
	}
	return f.prices[symbol], nil
}

func comparePlan() dca.SimulationInput {
	return dca.SimulationInput{
		StartDate: date("2023-01-02"), EndDate: date("2023-06-30"), Amount: 100, Frequency: dca.FrequencyMonthly,
	}
}

func TestCompare_SimulatesEachSymbolInRequestOrder(t *testing.T) {
	start, end := date("2023-01-02"), date("2023-06-30")
	uc := dcauc.NewSimulatorUseCase(&perSymbolFetcher{prices: map[string][]dca.HistoricalPrice{
		"AAA": buildPrices(start, end, 100),
		"BBB": risingPrices(start, end, 50),
	}})

	cmp, err := uc.Compare([]string{"BBB", "AAA"}, comparePlan())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmp.Results) != 2 || len(cmp.Skipped) != 0 {
		t.Fatalf("got %d results, %d skipped", len(cmp.Results), len(cmp.Skipped))
	}
	if cmp.Results[0].Symbol != "BBB" || cmp.Results[1].Symbol != "AAA" {
		t.Errorf("order not kept: %s, %s", cmp.Results[0].Symbol, cmp.Results[1].Symbol)
	}
	// One plan for all: the same amount, frequency and range.
	if cmp.Results[0].TotalInvested != cmp.Results[1].TotalInvested {
		t.Errorf("total invested differs: %f vs %f", cmp.Results[0].TotalInvested, cmp.Results[1].TotalInvested)
	}
	if cmp.Results[1].TotalReturnPct != 0 || cmp.Results[0].TotalReturnPct <= 0 {
		t.Errorf("returns wrong: flat %f, rising %f", cmp.Results[1].TotalReturnPct, cmp.Results[0].TotalReturnPct)
	}
}

func TestCompare_SkipsAssetsWithoutHistoryAndSaysWhich(t *testing.T) {
	start, end := date("2023-01-02"), date("2023-06-30")
	uc := dcauc.NewSimulatorUseCase(&perSymbolFetcher{
		prices: map[string][]dca.HistoricalPrice{
			"AAA": buildPrices(start, end, 100),
			// One price only: fewer than two buy dates.
			"NEW": {{Date: date("2023-06-01"), Close: 5}},
		},
		errs: map[string]error{"GONE": dca.ErrSymbolNotFound},
	})

	cmp, err := uc.Compare([]string{"AAA", "GONE", "NEW"}, comparePlan())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmp.Results) != 1 || cmp.Results[0].Symbol != "AAA" {
		t.Fatalf("results = %+v", cmp.Results)
	}
	if len(cmp.Skipped) != 2 || cmp.Skipped[0].Symbol != "GONE" || cmp.Skipped[1].Symbol != "NEW" {
		t.Fatalf("skipped = %+v", cmp.Skipped)
	}
	for _, s := range cmp.Skipped {
		if s.Reason == "" {
			t.Errorf("%s has no reason", s.Symbol)
		}
	}
}

func TestCompare_NothingSimulatedIsNotAnErrorUnlessTheProviderFailed(t *testing.T) {
	uc := dcauc.NewSimulatorUseCase(&perSymbolFetcher{errs: map[string]error{"A": dca.ErrSymbolNotFound, "B": dca.ErrSymbolNotFound}})
	cmp, err := uc.Compare([]string{"A", "B"}, comparePlan())
	if err != nil || len(cmp.Results) != 0 || len(cmp.Skipped) != 2 {
		t.Fatalf("no-history: %+v, %v", cmp, err)
	}

	boom := errors.New("upstream down")
	uc = dcauc.NewSimulatorUseCase(&perSymbolFetcher{errs: map[string]error{"A": boom, "B": dca.ErrSymbolNotFound}})
	if _, err := uc.Compare([]string{"A", "B"}, comparePlan()); !errors.Is(err, boom) {
		t.Fatalf("want the upstream error, got %v", err)
	}
}

func TestCompare_AnOutageOnOneAssetStillReturnsTheOthers(t *testing.T) {
	start, end := date("2023-01-02"), date("2023-06-30")
	uc := dcauc.NewSimulatorUseCase(&perSymbolFetcher{
		prices: map[string][]dca.HistoricalPrice{"AAA": buildPrices(start, end, 100)},
		errs:   map[string]error{"BBB": errors.New("timeout")},
	})
	cmp, err := uc.Compare([]string{"AAA", "BBB"}, comparePlan())
	if err != nil || len(cmp.Results) != 1 || len(cmp.Skipped) != 1 || cmp.Skipped[0].Symbol != "BBB" {
		t.Fatalf("got %+v, %v", cmp, err)
	}
}
