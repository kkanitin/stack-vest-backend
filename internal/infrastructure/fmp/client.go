package fmp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
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

type fmpQuote struct {
	Symbol         string  `json:"symbol"`
	Price          float64 `json:"price"`
	Change         float64 `json:"change"`
	ChangesPercent float64 `json:"changesPercentage"`
	Currency       string  `json:"currency"`
	Timestamp      int64   `json:"timestamp"`
}

func (c *Client) GetQuote(symbol string) (*stock.Quote, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("apikey", c.apiKey)

	resp, err := c.httpClient.Get(c.baseURL + "/quote?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("fmp request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw []fmpQuote
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fmp decode failed: %w", err)
	}
	if len(raw) == 0 {
		return nil, stock.ErrSymbolNotFound
	}

	r := raw[0]
	return &stock.Quote{
		Symbol:        r.Symbol,
		Price:         r.Price,
		Change:        r.Change,
		ChangePercent: r.ChangesPercent,
		Currency:      r.Currency,
		Timestamp:     time.Unix(r.Timestamp, 0).UTC(),
	}, nil
}

var _ stock.Quoter = (*Client)(nil)

type fmpHistoricalResponse struct {
	Symbol     string `json:"symbol"`
	Historical []struct {
		Date     string  `json:"date"`
		AdjClose float64 `json:"adjClose"`
		Close    float64 `json:"close"`
	} `json:"historical"`
}

func (c *Client) GetHistoricalPrices(symbol string, from, to time.Time) ([]dca.HistoricalPrice, error) {
	params := url.Values{}
	params.Set("from", from.Format("2006-01-02"))
	params.Set("to", to.Format("2006-01-02"))
	params.Set("apikey", c.apiKey)

	endpoint := fmt.Sprintf("%s/historical-price-eod/full/%s?%s", c.baseURL, symbol, params.Encode())
	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fmp request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw fmpHistoricalResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fmp decode failed: %w", err)
	}
	if len(raw.Historical) == 0 {
		return nil, dca.ErrSymbolNotFound
	}

	// FMP returns descending; reverse to ascending
	prices := make([]dca.HistoricalPrice, 0, len(raw.Historical))
	for i := len(raw.Historical) - 1; i >= 0; i-- {
		h := raw.Historical[i]
		t, err := time.Parse("2006-01-02", h.Date)
		if err != nil {
			continue
		}
		prices = append(prices, dca.HistoricalPrice{Date: t, AdjClose: h.AdjClose})
	}
	return prices, nil
}

var _ dca.PriceFetcher = (*Client)(nil)

func (c *Client) GetHistoryClose(symbol string, from, to time.Time) ([]stock.HistoryPoint, error) {
	params := url.Values{}
	params.Set("from", from.Format("2006-01-02"))
	params.Set("to", to.Format("2006-01-02"))
	params.Set("apikey", c.apiKey)

	endpoint := fmt.Sprintf("%s/historical-price-eod/full/%s?%s", c.baseURL, symbol, params.Encode())
	resp, err := c.httpClient.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("fmp request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw fmpHistoricalResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fmp decode failed: %w", err)
	}
	if len(raw.Historical) == 0 {
		return nil, stock.ErrSymbolNotFound
	}

	// FMP returns descending; reverse to ascending
	points := make([]stock.HistoryPoint, 0, len(raw.Historical))
	for i := len(raw.Historical) - 1; i >= 0; i-- {
		h := raw.Historical[i]
		points = append(points, stock.HistoryPoint{Date: h.Date, Close: h.Close})
	}
	return points, nil
}

var _ stock.HistoryFetcher = (*Client)(nil)

type fmpMostActive struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

// MostActiveStock is a minimal summary returned by GetMostActiveStocks.
type MostActiveStock struct {
	Symbol string
	Name   string
}

// GetMostActiveStocks returns the top n most-active stocks from FMP.
// Pass n ≤ 0 to return all results.
func (c *Client) GetMostActiveStocks(n int) ([]MostActiveStock, error) {
	params := url.Values{}
	params.Set("apikey", c.apiKey)

	resp, err := c.httpClient.Get(c.baseURL + "/most-actives?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("fmp request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw []fmpMostActive
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fmp decode failed: %w", err)
	}

	if n > 0 && n < len(raw) {
		raw = raw[:n]
	}

	result := make([]MostActiveStock, 0, len(raw))
	for _, r := range raw {
		result = append(result, MostActiveStock{Symbol: r.Symbol, Name: r.Name})
	}
	return result, nil
}
