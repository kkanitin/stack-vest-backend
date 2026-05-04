package stock

type Match struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Region      string `json:"region"`
	MarketOpen  string `json:"marketOpen"`
	MarketClose string `json:"marketClose"`
	Timezone    string `json:"timezone"`
	Currency    string `json:"currency"`
	MatchScore  string `json:"matchScore"`
}

type Searcher interface {
	SearchSymbol(keywords string) ([]Match, error)
}
