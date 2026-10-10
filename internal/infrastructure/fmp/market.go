package fmp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/kanitin/stackvest/backend/internal/domain/market"
)

// constituentPaths maps each index to its FMP constituents endpoint.
var constituentPaths = map[market.Index]string{
	market.IndexSP500:     "/sp500-constituent",
	market.IndexNasdaq100: "/nasdaq-constituent",
	market.IndexDow30:     "/dowjones-constituent",
}

// batchQuoteChunk is how many symbols one /batch-quote call asks for; it keeps
// the URL well under common proxy limits.
const batchQuoteChunk = 100

// quoteFallbackConcurrency bounds the per-symbol /quote calls made when
// /batch-quote is not on the plan.
const quoteFallbackConcurrency = 8

type fmpConstituent struct {
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	Sector    string `json:"sector"`
	SubSector string `json:"subSector"`
}

// ListConstituents returns the current members of an index from FMP.
func (c *Client) ListConstituents(index market.Index) ([]market.Constituent, error) {
	path, ok := constituentPaths[index]
	if !ok {
		return nil, market.ErrUnknownIndex
	}
	params := url.Values{}
	params.Set("apikey", c.apiKey)

	resp, err := c.doGet(c.baseURL + path + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := checkPlanStatus(resp); err != nil {
		return nil, err
	}

	var raw []fmpConstituent
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fmp decode failed: %w", err)
	}
	out := make([]market.Constituent, 0, len(raw))
	for _, r := range raw {
		if r.Symbol == "" {
			continue
		}
		out = append(out, market.Constituent{
			Symbol:    r.Symbol,
			Name:      r.Name,
			Sector:    r.Sector,
			SubSector: r.SubSector,
		})
	}
	return out, nil
}

var _ market.ConstituentLister = (*Client)(nil)

// fmpMarketQuote is the shape shared by /batch-quote and /quote. The stable API
// names the percentage "changePercentage"; older responses used
// "changesPercentage", so both are read.
type fmpMarketQuote struct {
	Symbol            string  `json:"symbol"`
	Price             float64 `json:"price"`
	ChangePercentage  float64 `json:"changePercentage"`
	ChangesPercentage float64 `json:"changesPercentage"`
	MarketCap         float64 `json:"marketCap"`
}

func (q fmpMarketQuote) toDomain() market.Quote {
	pct := q.ChangePercentage
	if pct == 0 {
		pct = q.ChangesPercentage
	}
	return market.Quote{Symbol: q.Symbol, Price: q.Price, ChangePercent: pct, MarketCap: q.MarketCap}
}

// GetBatchQuotes quotes many symbols via /batch-quote. If the plan does not
// include /batch-quote, it remembers that and uses one /quote call per symbol
// from then on.
func (c *Client) GetBatchQuotes(symbols []string) ([]market.Quote, error) {
	if !c.batchQuoteRestricted.Load() {
		quotes, err := c.batchQuotes(symbols)
		if err == nil {
			return quotes, nil
		}
		if err != market.ErrPlanRestricted {
			return nil, err
		}
		c.batchQuoteRestricted.Store(true)
	}
	return c.quotesOneByOne(symbols)
}

func (c *Client) batchQuotes(symbols []string) ([]market.Quote, error) {
	out := make([]market.Quote, 0, len(symbols))
	for start := 0; start < len(symbols); start += batchQuoteChunk {
		end := min(start+batchQuoteChunk, len(symbols))
		raw, err := c.fetchMarketQuotes("/batch-quote", "symbols", strings.Join(symbols[start:end], ","))
		if err != nil {
			return nil, err
		}
		for _, r := range raw {
			out = append(out, r.toDomain())
		}
	}
	return out, nil
}

func (c *Client) quotesOneByOne(symbols []string) ([]market.Quote, error) {
	results := make([]*market.Quote, len(symbols))
	var g errgroup.Group
	g.SetLimit(quoteFallbackConcurrency)
	for i, sym := range symbols {
		g.Go(func() error {
			raw, err := c.fetchMarketQuotes("/quote", "symbol", sym)
			if err != nil {
				return err
			}
			if len(raw) > 0 {
				q := raw[0].toDomain()
				results[i] = &q
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	out := make([]market.Quote, 0, len(symbols))
	for _, q := range results {
		if q != nil {
			out = append(out, *q)
		}
	}
	return out, nil
}

func (c *Client) fetchMarketQuotes(path, param, value string) ([]fmpMarketQuote, error) {
	params := url.Values{}
	params.Set(param, value)
	params.Set("apikey", c.apiKey)

	resp, err := c.doGet(c.baseURL + path + "?" + params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := checkPlanStatus(resp); err != nil {
		return nil, err
	}

	var raw []fmpMarketQuote
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("fmp decode failed: %w", err)
	}
	return raw, nil
}

var _ market.BatchQuoter = (*Client)(nil)

// checkPlanStatus maps FMP's "not on your plan" responses (402/403) to
// market.ErrPlanRestricted and any other non-2xx status to an error.
func checkPlanStatus(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusPaymentRequired || resp.StatusCode == http.StatusForbidden:
		_, _ = io.Copy(io.Discard, resp.Body)
		return market.ErrPlanRestricted
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("fmp unexpected status %d", resp.StatusCode)
	}
	return nil
}
