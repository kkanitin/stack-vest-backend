package dca

import (
	"errors"
	"time"
)

type Frequency string

const (
	FrequencyDaily    Frequency = "daily"
	FrequencyWeekly   Frequency = "weekly"
	FrequencyBiweekly Frequency = "biweekly"
	FrequencyMonthly  Frequency = "monthly"
)

func (f Frequency) IsValid() bool {
	switch f {
	case FrequencyDaily, FrequencyWeekly, FrequencyBiweekly, FrequencyMonthly:
		return true
	}
	return false
}

type SimulationInput struct {
	Symbol    string
	StartDate time.Time
	EndDate   time.Time
	Amount    float64
	Frequency Frequency
}

// HistoricalPrice is one daily close. Close is split-adjusted but not dividend-adjusted, so
// results reflect price movement only.
type HistoricalPrice struct {
	Date  time.Time
	Close float64
}

type DataPoint struct {
	Date           string  `json:"date"`
	Price          float64 `json:"price"`
	UnitsPurchased float64 `json:"unitsPurchased"`
	TotalUnits     float64 `json:"totalUnits"`
	TotalInvested  float64 `json:"totalInvested"`
	PortfolioValue float64 `json:"portfolioValue"`
	ReturnPct      float64 `json:"returnPct"`
}

type SimulationResult struct {
	Symbol               string    `json:"symbol"`
	StartDate            string    `json:"startDate"`
	EndDate              string    `json:"endDate"`
	Frequency            Frequency `json:"frequency"`
	AmountPerPeriod      float64   `json:"amountPerPeriod"`
	TotalInvested        float64   `json:"totalInvested"`
	FinalPortfolioValue  float64   `json:"finalPortfolioValue"`
	TotalReturn          float64   `json:"totalReturn"`
	TotalReturnPct       float64   `json:"totalReturnPct"`
	AnnualizedReturnPct  float64   `json:"annualizedReturnPct"`
	AnnualizedReturnNote string    `json:"annualizedReturnNote"`
	// MoneyWeightedReturnPct is the annualised IRR of the dated purchases (XIRR); nil when it cannot be solved.
	MoneyWeightedReturnPct  *float64    `json:"moneyWeightedReturnPct"`
	MoneyWeightedReturnNote string      `json:"moneyWeightedReturnNote"`
	PriceBasisNote          string      `json:"priceBasisNote"`
	PeriodsCount            int         `json:"periodsCount"`
	TotalUnits              float64     `json:"totalUnits"`
	DataPoints              []DataPoint `json:"dataPoints"`
}

// SkippedSymbol is an asset left out of a comparison, with a reason a user can read.
type SkippedSymbol struct {
	Symbol string `json:"symbol"`
	Reason string `json:"reason"`
}

// Comparison is one plan simulated over several assets. Results keep the request order;
// assets that could not be simulated are listed in Skipped instead.
type Comparison struct {
	Results []*SimulationResult `json:"results"`
	Skipped []SkippedSymbol     `json:"skipped"`
}

// Holding is one asset in a portfolio with its current weight. Weights are relative: only
// their proportions matter.
type Holding struct {
	Symbol string
	Weight float64
}

// PortfolioAsset is one holding's share of a holdings simulation. WeightPct is its share of
// the money actually invested (0-100), after assets that were skipped have been dropped.
// Result is scaled to that share, so the asset results add up to the combined one.
type PortfolioAsset struct {
	Symbol    string            `json:"symbol"`
	WeightPct float64           `json:"weightPct"`
	Result    *SimulationResult `json:"result"`
}

// PortfolioSimulation is the plan run across a portfolio's holdings at fixed weights: each
// purchase is split by weight. Combined sums the assets; Assets is the per-asset breakdown.
type PortfolioSimulation struct {
	Combined *SimulationResult `json:"combined"`
	Assets   []PortfolioAsset  `json:"assets"`
	Skipped  []SkippedSymbol   `json:"skipped"`
}

type PriceFetcher interface {
	GetHistoricalPrices(symbol string, from, to time.Time) ([]HistoricalPrice, error)
}

var (
	ErrSymbolNotFound    = errors.New("symbol not found")
	ErrDateRangeTooShort = errors.New("date range too short for the selected frequency")
)
