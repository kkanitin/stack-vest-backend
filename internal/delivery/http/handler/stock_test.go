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

type mockStockQuoteUC struct{}

func (m *mockStockQuoteUC) Execute(symbol string) (*domain.Quote, error) { return nil, nil }

type mockStockHistoryUC struct{}

func (m *mockStockHistoryUC) Execute(symbol string, r domain.HistoryRange) (*domain.History, error) {
	return nil, nil
}

func newStockRouter(uc stockSearchUseCase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewStockHandler(uc, &mockStockPriceChangeUC{}, &mockStockQuoteUC{}, &mockStockHistoryUC{}).RegisterRoutes(r.Group(""))
	return r
}

func TestSearch(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		mockErr  error
		wantCode int
	}{
		{"missing keywords", "/stocks/search", nil, http.StatusBadRequest},
		{"use-case error", "/stocks/search?keywords=AAPL", errors.New("upstream error"), http.StatusInternalServerError},
		{"success", "/stocks/search?keywords=AAPL", nil, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newStockRouter(&mockStockSearchUC{err: tc.mockErr})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}
