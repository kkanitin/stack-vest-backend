package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

type mockStockSearchUC struct {
	results []domain.Match
	err     error
}

func (m *mockStockSearchUC) Execute(keywords string) ([]domain.Match, error) {
	return m.results, m.err
}

type mockStockPriceChangeUC struct {
	result *domain.PriceChange
	err    error
}

func (m *mockStockPriceChangeUC) Execute(symbol string) (*domain.PriceChange, error) {
	return m.result, m.err
}

func newStockRouter(uc stockSearchUseCase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewStockHandler(uc, &mockStockPriceChangeUC{}).RegisterRoutes(r.Group(""))
	return r
}

func TestSearch_MissingKeywords(t *testing.T) {
	r := newStockRouter(&mockStockSearchUC{})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stocks/search", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestSearch_UseCaseError(t *testing.T) {
	r := newStockRouter(&mockStockSearchUC{err: errors.New("upstream error")})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stocks/search?keywords=AAPL", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestSearch_Success(t *testing.T) {
	matches := []domain.Match{{Symbol: "AAPL", Name: "Apple Inc."}}
	r := newStockRouter(&mockStockSearchUC{results: matches})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/stocks/search?keywords=AAPL", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
