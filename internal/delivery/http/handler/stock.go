package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
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
		response.Err(c, http.StatusBadRequest, "query parameter 'keywords' is required")
		return
	}

	results, err := h.searchUC.Execute(keywords)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "stock search failed", "keywords", keywords, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to search stocks")
		return
	}

	n := len(results)
	response.OKList(c, results, response.Meta{Total: n, Page: 1, Size: n, CurrentPageCount: n})
}
