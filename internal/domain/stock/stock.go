package stock

import (
	"errors"
	"time"
)

var ErrSymbolNotFound = errors.New("symbol not found")
var ErrTooManySymbols = errors.New("symbols must not exceed 10 items")

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
	SearchName(keywords string) ([]Match, error)
}

// SymbolLister returns the full set of tradable stock and ETF symbols. It backs
// the search use-case's stock/ETF membership filter.
type SymbolLister interface {
	ListStockSymbols() ([]string, error)
	ListETFSymbols() ([]string, error)
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

type BatchHistoryRange string

const (
	BatchRange7D  BatchHistoryRange = "7D"
	BatchRange30D BatchHistoryRange = "30D"
	BatchRange90D BatchHistoryRange = "90D"
	BatchRange1Y  BatchHistoryRange = "1Y"
	BatchRangeAll BatchHistoryRange = "All"
)

func (r BatchHistoryRange) IsValid() bool {
	switch r {
	case BatchRange7D, BatchRange30D, BatchRange90D, BatchRange1Y, BatchRangeAll:
		return true
	}
	return false
}

func (r BatchHistoryRange) Days() int {
	switch r {
	case BatchRange7D:
		return 7
	case BatchRange30D:
		return 30
	case BatchRange90D:
		return 90
	case BatchRange1Y:
		return 365
	case BatchRangeAll:
		return 1825
	}
	return 0
}

type BatchHistoryItem struct {
	Symbol string         `json:"symbol"`
	Range  string         `json:"range"`
	Points []HistoryPoint `json:"points"`
}

type CompanyProfile struct {
	Symbol            string  `json:"symbol"`
	CompanyName       string  `json:"companyName"`
	Currency          string  `json:"currency"`
	Exchange          string  `json:"exchange"`
	ExchangeFullName  string  `json:"exchangeFullName"`
	Industry          string  `json:"industry"`
	Sector            string  `json:"sector"`
	Country           string  `json:"country"`
	CEO               string  `json:"ceo"`
	Website           string  `json:"website"`
	Description       string  `json:"description"`
	Image             string  `json:"image"`
	Price             float64 `json:"price"`
	MarketCap         float64 `json:"marketCap"`
	Beta              float64 `json:"beta"`
	IPODate           string  `json:"ipoDate"`
	FullTimeEmployees string  `json:"fullTimeEmployees"`
	IsEtf             bool    `json:"isEtf"`
	IsActivelyTrading bool    `json:"isActivelyTrading"`
}

type ProfileFetcher interface {
	GetProfile(symbol string) (*CompanyProfile, error)
}
