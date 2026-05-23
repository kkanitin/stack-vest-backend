package stock

import (
	"errors"
	"time"
)

var ErrSymbolNotFound = errors.New("symbol not found")

type Match struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Region   string `json:"region"`
	Currency string `json:"currency"`
}

type PriceChange struct {
	Symbol string  `json:"symbol"`
	D1     float64 `json:"1D"`
	D5     float64 `json:"5D"`
	M1     float64 `json:"1M"`
	M3     float64 `json:"3M"`
	M6     float64 `json:"6M"`
	YTD    float64 `json:"ytd"`
	Y1     float64 `json:"1Y"`
	Y3     float64 `json:"3Y"`
	Y5     float64 `json:"5Y"`
	Y10    float64 `json:"10Y"`
	Max    float64 `json:"max"`
}

type Quote struct {
	Symbol        string    `json:"symbol"`
	Price         float64   `json:"price"`
	Change        float64   `json:"change"`
	ChangePercent float64   `json:"changePercent"`
	Currency      string    `json:"currency"`
	Timestamp     time.Time `json:"timestamp"`
}

type Searcher interface {
	SearchSymbol(keywords string) ([]Match, error)
}

type PriceChanger interface {
	GetPriceChange(symbol string) (*PriceChange, error)
}

type Quoter interface {
	GetQuote(symbol string) (*Quote, error)
}

type HistoryRange string

const (
	Range7D HistoryRange = "7d"
	Range1M HistoryRange = "1M"
	Range3M HistoryRange = "3M"
	Range6M HistoryRange = "6M"
	Range1Y HistoryRange = "1Y"
	Range5Y HistoryRange = "5Y"
)

func (r HistoryRange) IsValid() bool {
	switch r {
	case Range7D, Range1M, Range3M, Range6M, Range1Y, Range5Y:
		return true
	}
	return false
}

func (r HistoryRange) Days() int {
	switch r {
	case Range7D:
		return 7
	case Range1M:
		return 30
	case Range3M:
		return 90
	case Range6M:
		return 180
	case Range1Y:
		return 365
	case Range5Y:
		return 1825
	}
	return 0
}

type HistoryPoint struct {
	Date  string  `json:"date"`
	Close float64 `json:"close"`
}

type History struct {
	Symbol string         `json:"symbol"`
	Range  HistoryRange   `json:"range"`
	Points []HistoryPoint `json:"points"`
}

type HistoryFetcher interface {
	GetHistoryClose(symbol string, from, to time.Time) ([]HistoryPoint, error)
}
