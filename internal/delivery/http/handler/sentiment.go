package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	domain "github.com/kanitin/stackvest/backend/internal/domain/sentiment"
)

type sentimentUseCase interface {
	Execute(ctx context.Context) (*domain.Score, error)
}

type SentimentHandler struct {
	uc sentimentUseCase
}

func NewSentimentHandler(uc sentimentUseCase) *SentimentHandler {
	return &SentimentHandler{uc: uc}
}

func (h *SentimentHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/sentiment", h.Get)
}

func (h *SentimentHandler) Get(c *gin.Context) {
	result, err := h.uc.Execute(c.Request.Context())
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "sentiment fetch failed", "error", err)
		response.Err(c, http.StatusServiceUnavailable, "failed to fetch market sentiment")
		return
	}
	response.OK(c, result)
}
