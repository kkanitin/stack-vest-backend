package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
)

func holdingsPayload(holdings ...map[string]any) map[string]any {
	return map[string]any{"holdings": holdings, "startDate": "2023-01-02", "endDate": "2023-06-30", "amount": 100, "frequency": "monthly"}
}

func holding(symbol string, weight float64) map[string]any {
	return map[string]any{"symbol": symbol, "weight": weight}
}

func TestDCAHoldings_ReturnsCombinedBreakdownAndSkipped(t *testing.T) {
	w := postDCA(t, "/dca/holdings", holdingsPayload(holding("aaa", 60), holding("NOPE", 20), holding("bbb", 20)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Result dca.PortfolioSimulation `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	r := body.Result
	if r.Combined == nil || r.Combined.TotalInvested != 600 {
		t.Fatalf("combined = %+v", r.Combined)
	}
	if len(r.Assets) != 2 || r.Assets[0].Symbol != "AAA" || r.Assets[1].Symbol != "BBB" {
		t.Errorf("assets = %+v", r.Assets)
	}
	if got := r.Assets[0].WeightPct + r.Assets[1].WeightPct; got < 99.999 || got > 100.001 {
		t.Errorf("weights add up to %f, want 100", got)
	}
	if len(r.Skipped) != 1 || r.Skipped[0].Symbol != "NOPE" {
		t.Errorf("skipped = %+v", r.Skipped)
	}
}

func TestDCAHoldings_MergesTheSameSymbolListedTwice(t *testing.T) {
	w := postDCA(t, "/dca/holdings", holdingsPayload(holding("AAA", 1), holding("aaa", 1)))
	var body struct {
		Result dca.PortfolioSimulation `json:"result"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusOK || len(body.Result.Assets) != 1 {
		t.Fatalf("status %d, %d assets", w.Code, len(body.Result.Assets))
	}
}

func TestDCAHoldings_NoHistoryAnywhereIsEmptyNotAnError(t *testing.T) {
	w := postDCA(t, "/dca/holdings", holdingsPayload(holding("NOPE", 1)))
	var body struct {
		Result dca.PortfolioSimulation `json:"result"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusOK || body.Result.Combined != nil || len(body.Result.Skipped) != 1 {
		t.Fatalf("status %d, %+v", w.Code, body.Result)
	}
}

func TestDCAHoldings_Rejects(t *testing.T) {
	many := make([]map[string]any, 21)
	for i := range many {
		many[i] = holding(fmt.Sprintf("S%d", i), 1)
	}
	tests := []struct {
		name string
		body map[string]any
	}{
		{"no holdings", holdingsPayload()},
		{"more than 20 holdings", holdingsPayload(many...)},
		{"a zero weight", holdingsPayload(holding("AAA", 0))},
		{"a negative weight", holdingsPayload(holding("AAA", -1))},
		{"a blank symbol", holdingsPayload(holding("", 1))},
		{"a future end date", map[string]any{"holdings": []map[string]any{holding("AAA", 1)}, "startDate": "2023-01-02", "endDate": "2999-01-01", "amount": 100, "frequency": "monthly"}},
		{"zero amount", map[string]any{"holdings": []map[string]any{holding("AAA", 1)}, "startDate": "2023-01-02", "endDate": "2023-06-30", "amount": 0, "frequency": "monthly"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if w := postDCA(t, "/dca/holdings", tc.body); w.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}
