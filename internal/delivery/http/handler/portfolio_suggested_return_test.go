package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
)

func TestSuggestedReturnHandler_ForeignReturns404(t *testing.T) {
	repo := &mockPortfolioRepo{getPortfolio: func(id string) (*portfoliodomain.Portfolio, error) {
		return &portfoliodomain.Portfolio{ID: id, UserID: "another-user"}, nil
	}}
	w := httptest.NewRecorder()
	newPortfolioRouter(repo).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/foreign-id/suggested-return", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for another user's portfolio, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestSuggestedReturnHandler_MissingReturns404(t *testing.T) {
	w := httptest.NewRecorder()
	newPortfolioRouter(&mockPortfolioRepo{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/nope/suggested-return", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a missing portfolio, got %d", w.Code)
	}
}

// The test router has no quoter, so every holding is unpriced: the answer is a normal 200
// envelope saying there is no suggestion, not an error.
func TestSuggestedReturnHandler_NoSuggestionIs200(t *testing.T) {
	repo := &mockPortfolioRepo{
		getPortfolio: ownedPortfolio,
		listPositions: func(string) ([]*portfoliodomain.Position, error) {
			return []*portfoliodomain.Position{{Symbol: "AAPL", Shares: 2}}, nil
		},
	}
	w := httptest.NewRecorder()
	newPortfolioRouter(repo).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/portfolios/pf1/suggested-return", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var body struct {
		Result struct {
			Available    bool                             `json:"available"`
			AnnualReturn *float64                         `json:"annualReturn"`
			Lookback     *portfoliodomain.ReturnLookback  `json:"lookback"`
			Holdings     []any                            `json:"holdings"`
			Skipped      []portfoliodomain.SkippedHolding `json:"skipped"`
			Note         string                           `json:"note"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	r := body.Result
	if r.Available || r.AnnualReturn != nil || r.Lookback != nil || r.Holdings == nil || len(r.Holdings) != 0 || r.Note == "" {
		t.Fatalf("expected a no-suggestion result, got %+v", r)
	}
	if len(r.Skipped) != 1 || r.Skipped[0].Symbol != "AAPL" || r.Skipped[0].Code != portfoliodomain.SkipNoPrice {
		t.Fatalf("expected AAPL skipped for no price, got %+v", r.Skipped)
	}
}
