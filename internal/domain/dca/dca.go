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

type HistoricalPrice struct {
	Date     time.Time
	AdjClose float64
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
	Symbol               string      `json:"symbol"`
	StartDate            string      `json:"startDate"`
	EndDate              string      `json:"endDate"`
	Frequency            Frequency   `json:"frequency"`
	AmountPerPeriod      float64     `json:"amountPerPeriod"`
	TotalInvested        float64     `json:"totalInvested"`
	FinalPortfolioValue  float64     `json:"finalPortfolioValue"`
	TotalReturn          float64     `json:"totalReturn"`
	TotalReturnPct       float64     `json:"totalReturnPct"`
	AnnualizedReturnPct  float64     `json:"annualizedReturnPct"`
	AnnualizedReturnNote string      `json:"annualizedReturnNote"`
	PeriodsCount         int         `json:"periodsCount"`
	TotalUnits           float64     `json:"totalUnits"`
	DataPoints           []DataPoint `json:"dataPoints"`
}

type PriceFetcher interface {
	GetHistoricalPrices(symbol string, from, to time.Time) ([]HistoricalPrice, error)
}

var (
	ErrSymbolNotFound    = errors.New("symbol not found")
	ErrDateRangeTooShort = errors.New("date range too short for the selected frequency")
)
