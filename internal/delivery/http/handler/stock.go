package handler

import (
	"context"
	"errors"
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

type stockQuoteUseCase interface {
	Execute(symbol string) (*domain.Quote, error)
}

type stockHistoryUseCase interface {
	Execute(symbol string, r domain.HistoryRange) (*domain.History, error)
}

type stockBatchPriceChangeUseCase interface {
	Execute(ctx context.Context, symbolsParam string) ([]*domain.PriceChange, error)
}

type stockBatchHistoryUseCase interface {
	Execute(ctx context.Context, symbolsParam string, r domain.BatchHistoryRange) ([]domain.BatchHistoryItem, error)
}

type stockDetailUseCase interface {
	Execute(ctx context.Context, symbol string, r domain.DetailRange) (*domain.AssetDetail, error)
}

type StockHandler struct {
	searchUC           stockSearchUseCase
	priceChangeUC      stockPriceChangeUseCase
	quoteUC            stockQuoteUseCase
	historyUC          stockHistoryUseCase
	batchPriceChangeUC stockBatchPriceChangeUseCase
	batchHistoryUC     stockBatchHistoryUseCase
	detailUC           stockDetailUseCase
}

func NewStockHandler(
	s stockSearchUseCase,
	p stockPriceChangeUseCase,
	q stockQuoteUseCase,
	h stockHistoryUseCase,
	bp stockBatchPriceChangeUseCase,
	bh stockBatchHistoryUseCase,
	d stockDetailUseCase,
) *StockHandler {
	return &StockHandler{
		searchUC:           s,
		priceChangeUC:      p,
		quoteUC:            q,
		historyUC:          h,
		batchPriceChangeUC: bp,
		batchHistoryUC:     bh,
		detailUC:           d,
	}
}

func (h *StockHandler) RegisterRoutes(rg *gin.RouterGroup) {
	stocks := rg.Group("/stocks")
	stocks.GET("/search", h.Search)
	stocks.GET("/price-changes", h.GetBatchPriceChanges)
	stocks.GET("/history", h.GetBatchHistory)
	stocks.GET("/:symbol/price-change", h.GetPriceChange)
	stocks.GET("/:symbol/quote", h.GetQuote)
	stocks.GET("/:symbol/history", h.GetHistory)
	stocks.GET("/:symbol/detail", h.GetDetail)
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

func (h *StockHandler) GetQuote(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		response.Err(c, http.StatusBadRequest, "path parameter 'symbol' is required")
		return
	}

	result, err := h.quoteUC.Execute(symbol)
	if errors.Is(err, domain.ErrSymbolNotFound) {
		response.Err(c, http.StatusNotFound, "symbol not found: "+symbol)
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "stock quote failed", "symbol", symbol, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to get stock quote")
		return
	}

	response.OK(c, result)
}

func (h *StockHandler) GetHistory(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		response.Err(c, http.StatusBadRequest, "path parameter 'symbol' is required")
		return
	}

	rangeParam := c.Query("range")
	r := domain.HistoryRange(rangeParam)
	if !r.IsValid() {
		response.Err(c, http.StatusBadRequest, "range must be one of: 7d, 1M, 3M, 6M, 1Y, 5Y")
		return
	}

	result, err := h.historyUC.Execute(symbol, r)
	if errors.Is(err, domain.ErrSymbolNotFound) {
		response.Err(c, http.StatusNotFound, "symbol not found: "+symbol)
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "stock history failed", "symbol", symbol, "range", rangeParam, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to fetch history")
		return
	}

	response.OK(c, result)
}

func (h *StockHandler) GetBatchPriceChanges(c *gin.Context) {
	symbolsParam := c.Query("symbols")
	if symbolsParam == "" {
		response.Err(c, http.StatusBadRequest, "query parameter 'symbols' is required")
		return
	}

	result, err := h.batchPriceChangeUC.Execute(c.Request.Context(), symbolsParam)
	if errors.Is(err, domain.ErrTooManySymbols) {
		response.Err(c, http.StatusBadRequest, "symbols must not exceed 10 items")
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "batch price change failed", "symbols", symbolsParam, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to get batch price changes")
		return
	}

	response.OK(c, result)
}

func (h *StockHandler) GetBatchHistory(c *gin.Context) {
	symbolsParam := c.Query("symbols")
	if symbolsParam == "" {
		response.Err(c, http.StatusBadRequest, "query parameter 'symbols' is required")
		return
	}

	rangeParam := c.Query("range")
	r := domain.BatchHistoryRange(rangeParam)
	if !r.IsValid() {
		response.Err(c, http.StatusBadRequest, "range must be one of: 7D, 30D, 90D, 1Y, All")
		return
	}

	result, err := h.batchHistoryUC.Execute(c.Request.Context(), symbolsParam, r)
	if errors.Is(err, domain.ErrTooManySymbols) {
		response.Err(c, http.StatusBadRequest, "symbols must not exceed 10 items")
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "batch history failed", "symbols", symbolsParam, "range", rangeParam, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to fetch batch history")
		return
	}

	response.OK(c, result)
}

func (h *StockHandler) GetDetail(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		response.Err(c, http.StatusBadRequest, "path parameter 'symbol' is required")
		return
	}

	rangeParam := c.Query("range")
	r := domain.DetailRange(rangeParam)
	if !r.IsValid() {
		response.Err(c, http.StatusBadRequest, "range must be one of: 1D, 1W, 1M, 1Y, All")
		return
	}

	result, err := h.detailUC.Execute(c.Request.Context(), symbol, r)
	if errors.Is(err, domain.ErrSymbolNotFound) {
		response.Err(c, http.StatusNotFound, "symbol not found: "+symbol)
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "stock detail failed", "symbol", symbol, "range", rangeParam, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to fetch asset detail")
		return
	}

	response.OK(c, result)
}
