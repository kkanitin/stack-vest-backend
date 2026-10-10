package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kanitin/stackvest/backend/internal/domain/dca"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
)

type stubPrices map[string][]dca.HistoricalPrice

func (s stubPrices) GetHistoricalPrices(symbol string, _, _ time.Time) ([]dca.HistoricalPrice, error) {
	if p, ok := s[symbol]; ok {
		return p, nil
	}
	return nil, dca.ErrSymbolNotFound
}

func dcaPrices() stubPrices {
	var p []dca.HistoricalPrice
	for d := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC); d.Before(time.Date(2023, 7, 1, 0, 0, 0, 0, time.UTC)); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			p = append(p, dca.HistoricalPrice{Date: d, Close: 100})
		}
	}
	return stubPrices{"AAA": p, "BBB": p}
}

func postDCA(t *testing.T, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewDCAHandler(dcauc.NewSimulatorUseCase(dcaPrices())).RegisterRoutes(r.Group(""))
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func comparePayload(symbols ...string) map[string]any {
	return map[string]any{"symbols": symbols, "startDate": "2023-01-02", "endDate": "2023-06-30", "amount": 100, "frequency": "monthly"}
}

func TestDCACompare_ReturnsResultsAndSkippedInOneResponse(t *testing.T) {
	w := postDCA(t, "/dca/compare", comparePayload("aaa", "NOPE", "bbb"))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Result dca.Comparison `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Result.Results) != 2 || body.Result.Results[0].Symbol != "AAA" || body.Result.Results[1].Symbol != "BBB" {
		t.Errorf("results = %+v", body.Result.Results)
	}
	if len(body.Result.Skipped) != 1 || body.Result.Skipped[0].Symbol != "NOPE" {
		t.Errorf("skipped = %+v", body.Result.Skipped)
	}
}

func TestDCACompare_DuplicateSymbolsAreSimulatedOnce(t *testing.T) {
	w := postDCA(t, "/dca/compare", comparePayload("AAA", "aaa"))
	var body struct {
		Result dca.Comparison `json:"result"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusOK || len(body.Result.Results) != 1 {
		t.Fatalf("status %d, %d results", w.Code, len(body.Result.Results))
	}
}

func TestDCACompare_Rejects(t *testing.T) {
	one := func(m map[string]any) map[string]any {
		m["symbols"] = []string{"AAA"}
		return m
	}
	tests := []struct {
		name string
		body map[string]any
	}{
		{"a fourth asset", comparePayload("A", "B", "C", "D")},
		{"no symbols", comparePayload()},
		{"a blank symbol", comparePayload("AAA", "")},
		{"zero amount", one(map[string]any{"startDate": "2023-01-02", "endDate": "2023-06-30", "amount": 0, "frequency": "monthly"})},
		{"a future end date", one(map[string]any{"startDate": "2023-01-02", "endDate": "2999-01-01", "amount": 100, "frequency": "monthly"})},
		{"a range over the limit", one(map[string]any{"startDate": "2000-01-02", "endDate": "2023-06-30", "amount": 100, "frequency": "daily"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if w := postDCA(t, "/dca/compare", tc.body); w.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestDCASimulate_StillValidatesThroughTheSharedPlanChecks(t *testing.T) {
	plan := func(symbol, start, end string) map[string]any {
		return map[string]any{"symbol": symbol, "startDate": start, "endDate": end, "amount": 100, "frequency": "monthly"}
	}
	if w := postDCA(t, "/dca/simulate", plan("AAA", "2023-01-02", "2023-06-30")); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if w := postDCA(t, "/dca/simulate", plan("AAA", "2023-06-30", "2023-01-02")); w.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", w.Code)
	}
	if w := postDCA(t, "/dca/simulate", plan("ZZZ", "2023-01-02", "2023-06-30")); w.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", w.Code)
	}
}
