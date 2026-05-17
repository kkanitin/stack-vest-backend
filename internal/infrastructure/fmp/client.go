package fmp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/kanitin/stackvest/backend/internal/domain/stock"
)

type Client struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		httpClient: &http.Client{},
		baseURL:    "https://financialmodelingprep.com/stable",
	}
}

type fmpSearchResult struct {
	Symbol           string `json:"symbol"`
	Name             string `json:"name"`
	Currency         string `json:"currency"`
	ExchangeFullName string `json:"exchangeFullName"`
	Exchange         string `json:"exchange"`
}

func (c *Client) SearchSymbol(keywords string) ([]stock.Match, error) {
	params := url.Values{}
	params.Set("query", keywords)
	params.Set("apikey", c.apiKey)

	resp, err := c.httpClient.Get(c.baseURL + "/search-symbol?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("fmp request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw []fmpSearchResult
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fmp decode failed: %w", err)
	}

	matches := make([]stock.Match, 0, len(raw))
	for _, r := range raw {
		matches = append(matches, stock.Match{
			Symbol:   r.Symbol,
			Name:     r.Name,
			Type:     r.Exchange,
			Region:   r.ExchangeFullName,
			Currency: r.Currency,
		})
	}
	return matches, nil
}

var _ stock.Searcher = (*Client)(nil)

type fmpPriceChange struct {
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

func (c *Client) GetPriceChange(symbol string) (*stock.PriceChange, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("apikey", c.apiKey)

	resp, err := c.httpClient.Get(c.baseURL + "/stock-price-change?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("fmp request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw []fmpPriceChange
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fmp decode failed: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}

	r := raw[0]
	return &stock.PriceChange{
		Symbol: r.Symbol,
		D1:     r.D1,
		D5:     r.D5,
		M1:     r.M1,
		M3:     r.M3,
		M6:     r.M6,
		YTD:    r.YTD,
		Y1:     r.Y1,
		Y3:     r.Y3,
		Y5:     r.Y5,
		Y10:    r.Y10,
		Max:    r.Max,
	}, nil
}

var _ stock.PriceChanger = (*Client)(nil)
