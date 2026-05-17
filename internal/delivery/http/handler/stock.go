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

type stockPriceChangeUseCase interface {
	Execute(symbol string) (*domain.PriceChange, error)
}

type StockHandler struct {
	searchUC      stockSearchUseCase
	priceChangeUC stockPriceChangeUseCase
}

func NewStockHandler(s stockSearchUseCase, p stockPriceChangeUseCase) *StockHandler {
	return &StockHandler{searchUC: s, priceChangeUC: p}
}

func (h *StockHandler) RegisterRoutes(rg *gin.RouterGroup) {
	stocks := rg.Group("/stocks")
	stocks.GET("/search", h.Search)
	stocks.GET("/:symbol/price-change", h.GetPriceChange)
}

func (h *StockHandler) Search(c *gin.Context) {
	keywords := c.Query("keywords")
	if keywords == "" {
		response.Err(c, http.StatusBadRequest, "query parameter 'keywords' is required")
		return
	}

	page, size, ok := parsePagination(c)
	if !ok {
		return
	}

	all, err := h.searchUC.Execute(keywords)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "stock search failed", "keywords", keywords, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to search stocks")
		return
	}

	total := len(all)
	offset := (page - 1) * size
	if offset > total {
		offset = total
	}
	end := offset + size
	if end > total {
		end = total
	}
	results := all[offset:end]
	currentPageCount := len(results)

	response.OKList(c, results, response.Meta{
		Total:            &total,
		Page:             &page,
		Size:             &size,
		CurrentPageCount: &currentPageCount,
	})
}

func (h *StockHandler) GetPriceChange(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		response.Err(c, http.StatusBadRequest, "path parameter 'symbol' is required")
		return
	}

	result, err := h.priceChangeUC.Execute(symbol)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "stock price change failed", "symbol", symbol, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to get stock price change")
		return
	}

	response.OK(c, result)
}
