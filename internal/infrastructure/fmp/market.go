package fmp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/kanitin/stackvest/backend/internal/domain/market"
)

// constituentPaths maps each index to its FMP constituents endpoint.
var constituentPaths = map[market.Index]string{
	market.IndexSP500:     "/sp500-constituent",
	market.IndexNasdaq100: "/nasdaq-constituent",
	market.IndexDow30:     "/dowjones-constituent",
}

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
