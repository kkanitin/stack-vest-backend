package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domain "github.com/kanitin/stackvest/backend/internal/domain/sentiment"
)

type mockSentimentUC struct {
	result *domain.Score
	err    error
}

func (m *mockSentimentUC) Execute(ctx context.Context) (*domain.Score, error) {
	return m.result, m.err
}

func newSentimentRouter(uc sentimentUseCase) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewSentimentHandler(uc).RegisterRoutes(r.Group(""))
	return r
}

func TestSentimentGet(t *testing.T) {
	tests := []struct {
		name       string
		mockResult *domain.Score
		mockErr    error
		wantCode   int
	}{
		{
			"success",
			&domain.Score{
				Score:  62,
				Status: "Greed",
				Signals: domain.Signals{
					VIX:                18.5,
					IndexChangePercent: 0.8,
					GainersCount:       30,
					LosersCount:        20,
				},
				Timestamp: time.Now().UTC(),
			},
			nil,
			http.StatusOK,
		},
		{"use-case error", nil, errors.New("upstream error"), http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newSentimentRouter(&mockSentimentUC{result: tc.mockResult, err: tc.mockErr})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sentiment", nil))
			if w.Code != tc.wantCode {
				t.Fatalf("expected %d, got %d", tc.wantCode, w.Code)
			}
		})
	}
}
