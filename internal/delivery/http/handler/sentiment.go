package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	domain "github.com/kanitin/stackvest/backend/internal/domain/sentiment"
	"github.com/kanitin/stackvest/backend/pkg/logger"
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
		zap.L().Error("sentiment fetch failed", logger.RequestID(c.Request.Context()), zap.Error(err))
		response.Err(c, http.StatusServiceUnavailable, "failed to fetch market sentiment")
		return
	}
	response.OK(c, result)
}
