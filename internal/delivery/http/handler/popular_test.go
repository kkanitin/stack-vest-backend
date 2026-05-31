package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	fmp "github.com/kanitin/stackvest/backend/internal/infrastructure/fmp"
)

// mockStockFetcher satisfies the stockFetcher interface for tests.
type mockStockFetcher struct {
	stocks []fmp.MostActiveStock
	err    error
}

func (m *mockStockFetcher) GetMostActiveStocks(_ int) ([]fmp.MostActiveStock, error) {
	return m.stocks, m.err
}

var testStocks = []fmp.MostActiveStock{
	{Symbol: "AAPL", Name: "Apple Inc."},
	{Symbol: "NVDA", Name: "NVIDIA Corporation"},
	{Symbol: "MSFT", Name: "Microsoft Corporation"},
	{Symbol: "AMZN", Name: "Amazon.com Inc."},
	{Symbol: "TSLA", Name: "Tesla Inc."},
}

func newPopularRouter(f stockFetcher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewPopularHandler(f).RegisterRoutes(r.Group(""))
	return r
}

func getPopular(t *testing.T, r *gin.Engine, query string) (int, struct {
	Results []popularEntry `json:"results"`
	Code    int            `json:"code"`
	Meta    struct {
		Total            *int `json:"total"`
		CurrentPageCount *int `json:"currentPageCount"`
	} `json:"meta"`
}) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/popular"+query, nil)
	r.ServeHTTP(w, req)
	var body struct {
		Results []popularEntry `json:"results"`
		Code    int            `json:"code"`
		Meta    struct {
			Total            *int `json:"total"`
			CurrentPageCount *int `json:"currentPageCount"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	return w.Code, body
}

// --- backward-compat tests (default type=crypto, no FMP call) ---

func TestPopular_ReturnsOK(t *testing.T) {
	r := newPopularRouter(nil)
	code, body := getPopular(t, r, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(body.Results) == 0 {
		t.Fatal("expected non-empty popular assets list")
	}
	if body.Meta.Total == nil || *body.Meta.Total == 0 {
		t.Fatal("expected non-zero total in meta")
	}
	if *body.Meta.CurrentPageCount != len(body.Results) {
		t.Errorf("currentPageCount %d does not match results length %d", *body.Meta.CurrentPageCount, len(body.Results))
	}
}

func TestPopular_EachEntryHasRequiredFields(t *testing.T) {
	r := newPopularRouter(nil)
	_, body := getPopular(t, r, "")
	for _, entry := range body.Results {
		if entry.Symbol == "" {
			t.Errorf("entry missing symbol: %+v", entry)
		}
		if entry.Name == "" {
			t.Errorf("entry missing name: %+v", entry)
		}
		if entry.Type == "" {
			t.Errorf("entry missing type: %+v", entry)
		}
		if entry.Category == nil {
			t.Errorf("entry has nil category for symbol %s", entry.Symbol)
		}
	}
}

func TestPopular_CategoryContentsAreValid(t *testing.T) {
	validCategories := map[string]bool{"Top 100": true, "DeFi": true, "L1s": true}
	for _, entry := range popularAssets {
		for _, cat := range entry.Category {
			if !validCategories[cat] {
				t.Errorf("symbol %s has unknown category %q", entry.Symbol, cat)
			}
		}
	}
}

// --- type=crypto ---

func TestPopular_TypeCrypto_ExplicitParam(t *testing.T) {
	r := newPopularRouter(nil)
	code, body := getPopular(t, r, "?type=crypto")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	for _, e := range body.Results {
		if e.Type != "crypto" {
			t.Errorf("expected crypto type, got %q for symbol %s", e.Type, e.Symbol)
		}
	}
}

// --- type=stock ---

func TestPopular_TypeStock_FMPSuccess(t *testing.T) {
	r := newPopularRouter(&mockStockFetcher{stocks: testStocks})
	code, body := getPopular(t, r, "?type=stock")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(body.Results) != len(testStocks) {
		t.Fatalf("expected %d stocks, got %d", len(testStocks), len(body.Results))
	}
	for _, e := range body.Results {
		if e.Type != "stock" {
			t.Errorf("expected stock type, got %q for symbol %s", e.Type, e.Symbol)
		}
		if len(e.Category) == 0 || e.Category[0] != "Most Active" {
			t.Errorf("expected 'Most Active' category for %s", e.Symbol)
		}
	}
}

func TestPopular_TypeStock_FMPFails_ReturnsEmpty(t *testing.T) {
	r := newPopularRouter(&mockStockFetcher{err: fmt.Errorf("fmp unavailable")})
	code, body := getPopular(t, r, "?type=stock")
	if code != http.StatusOK {
		t.Fatalf("expected graceful 200, got %d", code)
	}
	if len(body.Results) != 0 {
		t.Errorf("expected empty results on FMP failure, got %d", len(body.Results))
	}
}

// --- type=all ---

func TestPopular_TypeAll_FMPSuccess_ReturnsMix(t *testing.T) {
	r := newPopularRouter(&mockStockFetcher{stocks: testStocks})
	code, body := getPopular(t, r, "?type=all")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	hasCrypto, hasStock := false, false
	for _, e := range body.Results {
		if e.Type == "crypto" {
			hasCrypto = true
		}
		if e.Type == "stock" {
			hasStock = true
		}
	}
	if !hasCrypto {
		t.Error("expected crypto entries in type=all response")
	}
	if !hasStock {
		t.Error("expected stock entries in type=all response")
	}
}

func TestPopular_TypeAll_ProportionalSplit(t *testing.T) {
	// limit=10 → ceiling(10*0.6)=6 crypto, 4 stocks
	r := newPopularRouter(&mockStockFetcher{stocks: testStocks})
	code, body := getPopular(t, r, "?type=all&limit=10")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	var cryptoCount, stockCount int
	for _, e := range body.Results {
		if e.Type == "crypto" {
			cryptoCount++
		} else if e.Type == "stock" {
			stockCount++
		}
	}
	if cryptoCount != 6 {
		t.Errorf("expected 6 crypto entries for limit=10, got %d", cryptoCount)
	}
	if stockCount != 4 {
		t.Errorf("expected 4 stock entries for limit=10, got %d", stockCount)
	}
	if len(body.Results) != 10 {
		t.Errorf("expected 10 total results, got %d", len(body.Results))
	}
}

func TestPopular_TypeAll_FMPFails_ReturnsCryptoOnly(t *testing.T) {
	r := newPopularRouter(&mockStockFetcher{err: fmt.Errorf("fmp unavailable")})
	code, body := getPopular(t, r, "?type=all")
	if code != http.StatusOK {
		t.Fatalf("expected graceful 200, got %d", code)
	}
	for _, e := range body.Results {
		if e.Type != "crypto" {
			t.Errorf("expected only crypto on FMP failure, got type %q for %s", e.Type, e.Symbol)
		}
	}
	if len(body.Results) == 0 {
		t.Error("expected crypto fallback entries")
	}
}

// --- limit param ---

func TestPopular_LimitParam_CapsCryptoResults(t *testing.T) {
	r := newPopularRouter(nil)
	code, body := getPopular(t, r, "?limit=3")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(body.Results) != 3 {
		t.Errorf("expected 3 results with limit=3, got %d", len(body.Results))
	}
}

func TestPopular_LimitParam_CapsStockResults(t *testing.T) {
	r := newPopularRouter(&mockStockFetcher{stocks: testStocks})
	code, body := getPopular(t, r, "?type=stock&limit=2")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(body.Results) != 2 {
		t.Errorf("expected 2 results with limit=2, got %d", len(body.Results))
	}
}

// --- validation ---

func TestPopular_BadParam_Returns400(t *testing.T) {
	tests := []struct{ name, query string }{
		{"invalid type", "?type=bonds"},
		{"limit abc", "?limit=abc"},
		{"limit 0", "?limit=0"},
		{"limit -1", "?limit=-1"},
		{"limit 51", "?limit=51"},
		{"limit 100", "?limit=100"},
	}
	r := newPopularRouter(nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/popular"+tc.query, nil)
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", w.Code)
			}
		})
	}
}
