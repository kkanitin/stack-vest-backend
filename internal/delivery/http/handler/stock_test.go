package handler

import (
	"context"
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

type mockStockBatchPriceChangeUC struct {
	result []*domain.PriceChange
	err    error
}

func (m *mockStockBatchPriceChangeUC) Execute(ctx context.Context, symbolsParam string) ([]*domain.PriceChange, error) {
	return m.result, m.err
}

type mockStockBatchHistoryUC struct {
	result []domain.BatchHistoryItem
	err    error
}

func (m *mockStockBatchHistoryUC) Execute(ctx context.Context, symbolsParam string, r domain.BatchHistoryRange) ([]domain.BatchHistoryItem, error) {
	return m.result, m.err
}

type mockStockProfileUC struct {
	result *domain.CompanyProfile
	err    error
}

func (m *mockStockProfileUC) Execute(symbol string) (*domain.CompanyProfile, error) {
	return m.result, m.err
}

func newStockRouter(uc stockSearchUseCase) *gin.Engine {
	return newStockRouterFull(uc, &mockStockPriceChangeUC{}, &mockStockBatchPriceChangeUC{}, &mockStockBatchHistoryUC{}, &mockStockProfileUC{})
}

func newStockRouterFull(
	search stockSearchUseCase,
	priceChange stockPriceChangeUseCase,
	batchPriceChange stockBatchPriceChangeUseCase,
	batchHistory stockBatchHistoryUseCase,
	profile stockProfileUseCase,
) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewStockHandler(
		search,
		priceChange,
		&mockStockQuoteUC{},
		&mockStockHistoryUC{},
		batchPriceChange,
		batchHistory,
		profile,
	).RegisterRoutes(r.Group(""))
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

func TestGetBatchPriceChanges(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		mockErr  error
		wantCode int
	}{
		{"missing symbols", "/stocks/price-changes", nil, http.StatusBadRequest},
		{"too many symbols", "/stocks/price-changes?symbols=A,B", domain.ErrTooManySymbols, http.StatusBadRequest},
		{"use-case error", "/stocks/price-changes?symbols=AAPL", errors.New("upstream error"), http.StatusInternalServerError},
		{"success", "/stocks/price-changes?symbols=AAPL,MSFT", nil, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newStockRouterFull(&mockStockSearchUC{}, &mockStockPriceChangeUC{}, &mockStockBatchPriceChangeUC{err: tc.mockErr}, &mockStockBatchHistoryUC{}, &mockStockProfileUC{})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}

func TestGetBatchHistory(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		mockErr  error
		wantCode int
	}{
		{"missing symbols", "/stocks/history", nil, http.StatusBadRequest},
		{"missing range", "/stocks/history?symbols=AAPL", nil, http.StatusBadRequest},
		{"invalid range", "/stocks/history?symbols=AAPL&range=invalid", nil, http.StatusBadRequest},
		{"too many symbols", "/stocks/history?symbols=A,B&range=7D", domain.ErrTooManySymbols, http.StatusBadRequest},
		{"use-case error", "/stocks/history?symbols=AAPL&range=7D", errors.New("upstream error"), http.StatusInternalServerError},
		{"success", "/stocks/history?symbols=AAPL,MSFT&range=7D", nil, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newStockRouterFull(&mockStockSearchUC{}, &mockStockPriceChangeUC{}, &mockStockBatchPriceChangeUC{}, &mockStockBatchHistoryUC{err: tc.mockErr}, &mockStockProfileUC{})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}

func TestGetProfile(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		mockErr  error
		wantCode int
	}{
		{"symbol not found", "/stocks/ZZZZ/profile", domain.ErrSymbolNotFound, http.StatusNotFound},
		{"use-case error", "/stocks/AAPL/profile", errors.New("upstream error"), http.StatusInternalServerError},
		{"success", "/stocks/AAPL/profile", nil, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newStockRouterFull(&mockStockSearchUC{}, &mockStockPriceChangeUC{}, &mockStockBatchPriceChangeUC{}, &mockStockBatchHistoryUC{}, &mockStockProfileUC{result: &domain.CompanyProfile{}, err: tc.mockErr})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}

func TestGetPriceChange(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		mockErr  error
		wantCode int
	}{
		{"symbol not found", "/stocks/ZZZZ/price-change", domain.ErrSymbolNotFound, http.StatusNotFound},
		{"use-case error", "/stocks/AAPL/price-change", errors.New("upstream error"), http.StatusInternalServerError},
		{"success", "/stocks/AAPL/price-change", nil, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newStockRouterFull(&mockStockSearchUC{}, &mockStockPriceChangeUC{result: &domain.PriceChange{}, err: tc.mockErr}, &mockStockBatchPriceChangeUC{}, &mockStockBatchHistoryUC{}, &mockStockProfileUC{})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.url, nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}
