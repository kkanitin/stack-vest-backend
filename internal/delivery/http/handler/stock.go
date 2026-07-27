package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	domain "github.com/kanitin/stackvest/backend/internal/domain/stock"
	"github.com/kanitin/stackvest/backend/pkg/logger"
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

type stockProfileUseCase interface {
	Execute(symbol string) (*domain.CompanyProfile, error)
}

type StockHandler struct {
	searchUC           stockSearchUseCase
	priceChangeUC      stockPriceChangeUseCase
	quoteUC            stockQuoteUseCase
	historyUC          stockHistoryUseCase
	batchPriceChangeUC stockBatchPriceChangeUseCase
	batchHistoryUC     stockBatchHistoryUseCase
	profileUC          stockProfileUseCase
}

func NewStockHandler(
	s stockSearchUseCase,
	p stockPriceChangeUseCase,
	q stockQuoteUseCase,
	h stockHistoryUseCase,
	bp stockBatchPriceChangeUseCase,
	bh stockBatchHistoryUseCase,
	pr stockProfileUseCase,
) *StockHandler {
	return &StockHandler{
		searchUC:           s,
		priceChangeUC:      p,
		quoteUC:            q,
		historyUC:          h,
		batchPriceChangeUC: bp,
		batchHistoryUC:     bh,
		profileUC:          pr,
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
	stocks.GET("/:symbol/profile", h.GetProfile)
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
		zap.L().Error("stock search failed", logger.RequestID(c.Request.Context()), zap.String("keywords", keywords), zap.Error(err))
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

	response.OKList(
		c, results, response.Meta{
			Total:            &total,
			Page:             &page,
			Size:             &size,
			CurrentPageCount: &currentPageCount,
		},
	)
}

func (h *StockHandler) GetPriceChange(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		response.Err(c, http.StatusBadRequest, "path parameter 'symbol' is required")
		return
	}

	result, err := h.priceChangeUC.Execute(symbol)
	if errors.Is(err, domain.ErrSymbolNotFound) {
		response.Err(c, http.StatusNotFound, "symbol not found: "+symbol)
		return
	}
	if err != nil {
		zap.L().Error("stock price change failed", logger.RequestID(c.Request.Context()), zap.String("symbol", symbol), zap.Error(err))
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
		zap.L().Error("stock quote failed", logger.RequestID(c.Request.Context()), zap.String("symbol", symbol), zap.Error(err))
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
		zap.L().Error(
			"stock history failed",
			logger.RequestID(c.Request.Context()), zap.String("symbol", symbol), zap.String("range", rangeParam), zap.Error(err),
		)
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
		zap.L().Error("batch price change failed", logger.RequestID(c.Request.Context()), zap.String("symbols", symbolsParam), zap.Error(err))
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
		zap.L().Error(
			"batch history failed",
			logger.RequestID(c.Request.Context()), zap.String("symbols", symbolsParam), zap.String("range", rangeParam), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to fetch batch history")
		return
	}

	response.OK(c, result)
}

func (h *StockHandler) GetProfile(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		response.Err(c, http.StatusBadRequest, "path parameter 'symbol' is required")
		return
	}

	result, err := h.profileUC.Execute(symbol)
	if errors.Is(err, domain.ErrSymbolNotFound) {
		response.Err(c, http.StatusNotFound, "symbol not found: "+symbol)
		return
	}
	if err != nil {
		zap.L().Error("stock profile failed", logger.RequestID(c.Request.Context()), zap.String("symbol", symbol), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to get stock profile")
		return
	}

	response.OK(c, result)
}
