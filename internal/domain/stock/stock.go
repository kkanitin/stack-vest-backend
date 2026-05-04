package stock

type Match struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Region      string `json:"region"`
	MarketOpen  string `json:"market_open"`
	MarketClose string `json:"market_close"`
	Timezone    string `json:"timezone"`
	Currency    string `json:"currency"`
	MatchScore  string `json:"match_score"`
}

type Searcher interface {
	SearchSymbol(keywords string) ([]Match, error)
}
