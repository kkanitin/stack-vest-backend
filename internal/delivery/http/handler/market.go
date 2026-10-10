package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	domain "github.com/kanitin/stackvest/backend/internal/domain/market"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

type heatmapUseCase interface {
	Get(index domain.Index) (*domain.Heatmap, error)
}

type MarketHandler struct {
	heatmap heatmapUseCase
}

func NewMarketHandler(heatmap heatmapUseCase) *MarketHandler {
	return &MarketHandler{heatmap: heatmap}
}

func (h *MarketHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/market/heatmap", h.Heatmap)
}

type heatmapQuery struct {
	Index string `form:"index" binding:"required,oneof=sp500 nasdaq100 dow30"`
}

func (h *MarketHandler) Heatmap(c *gin.Context) {
	var q heatmapQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Err(c, http.StatusBadRequest, "index must be one of sp500, nasdaq100, dow30")
		return
	}

	hm, err := h.heatmap.Get(domain.Index(q.Index))
	if errors.Is(err, domain.ErrHeatmapNotReady) {
		c.Header("Retry-After", "30")
		response.Err(c, http.StatusServiceUnavailable, "heatmap is warming up, try again shortly")
		return
	}
	if err != nil {
		zap.L().Error("heatmap fetch failed", logger.RequestID(c.Request.Context()), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to fetch heatmap")
		return
	}
	response.OK(c, hm)
}
