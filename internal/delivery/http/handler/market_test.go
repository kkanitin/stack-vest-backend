package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domain "github.com/kanitin/stackvest/backend/internal/domain/market"
)

type mockHeatmapUC struct {
	result *domain.Heatmap
	err    error
	got    domain.Index
}

func (m *mockHeatmapUC) Get(index domain.Index) (*domain.Heatmap, error) {
	m.got = index
	return m.result, m.err
}

func TestMarketHeatmap(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		result   *domain.Heatmap
		err      error
		wantCode int
	}{
		{"success", "?index=sp500", &domain.Heatmap{Index: domain.IndexSP500}, nil, http.StatusOK},
		{"missing index", "", nil, nil, http.StatusBadRequest},
		{"unknown index", "?index=ftse", nil, nil, http.StatusBadRequest},
		{"warming up", "?index=dow30", nil, domain.ErrHeatmapNotReady, http.StatusServiceUnavailable},
		{"other error", "?index=nasdaq100", nil, errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			uc := &mockHeatmapUC{result: tc.result, err: tc.err}
			NewMarketHandler(uc).RegisterRoutes(r.Group(""))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/market/heatmap"+tc.query, nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
			if tc.wantCode == http.StatusServiceUnavailable && w.Header().Get("Retry-After") == "" {
				t.Error("expected a Retry-After header")
			}
		})
	}
}
