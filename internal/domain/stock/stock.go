package stock

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

type Searcher interface {
	SearchSymbol(keywords string) ([]Match, error)
}

type PriceChanger interface {
	GetPriceChange(symbol string) (*PriceChange, error)
}
