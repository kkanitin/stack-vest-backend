package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
)

type stockSearchUseCase interface {
	Execute(keywords string) ([]domain.Match, error)
}

type StockHandler struct {
	searchUC stockSearchUseCase
}

func NewStockHandler(s stockSearchUseCase) *StockHandler {
	return &StockHandler{searchUC: s}
}

func (h *StockHandler) RegisterRoutes(rg *gin.RouterGroup) {
	stocks := rg.Group("/stocks")
	stocks.GET("/search", h.Search)
}

func (h *StockHandler) Search(c *gin.Context) {
	keywords := c.Query("keywords")
	if keywords == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query parameter 'keywords' is required"})
		return
	}

	results, err := h.searchUC.Execute(keywords)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "stock search failed", "keywords", keywords, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to search stocks"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}
