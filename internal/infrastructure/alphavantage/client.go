package alphavantage

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
		baseURL:    "https://www.alphavantage.co/query",
	}
}

type alphaSearchResponse struct {
	BestMatches []map[string]string `json:"bestMatches"`
}

func (c *Client) SearchSymbol(keywords string) ([]stock.Match, error) {
	params := url.Values{}
	params.Set("function", "SYMBOL_SEARCH")
	params.Set("keywords", keywords)
	params.Set("apikey", c.apiKey)

	resp, err := c.httpClient.Get(c.baseURL + "?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("alphavantage request failed: %w", err)
	}
	defer resp.Body.Close()

	var raw alphaSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("alphavantage decode failed: %w", err)
	}

	matches := make([]stock.Match, 0, len(raw.BestMatches))
	for _, m := range raw.BestMatches {
		matches = append(
			matches, stock.Match{
				Symbol:      m["1. symbol"],
				Name:        m["2. name"],
				Type:        m["3. type"],
				Region:      m["4. region"],
				MarketOpen:  m["5. marketOpen"],
				MarketClose: m["6. marketClose"],
				Timezone:    m["7. timezone"],
				Currency:    m["8. currency"],
				MatchScore:  m["9. matchScore"],
			},
		)
	}
	return matches, nil
}

func (c *Client) GetStockQuote(symbol string) (any, error) {
	// TODO: Implement
	return nil, nil
}
