package handler

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	analysisdomain "github.com/kanitin/stackvest/backend/internal/domain/analysis"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	analysisuc "github.com/kanitin/stackvest/backend/internal/usecase/analysis"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

type PortfolioHandler struct {
	uc        *portfoliouc.UseCase
	analyzeUC *analysisuc.UseCase
}

func NewPortfolioHandler(uc *portfoliouc.UseCase, analyzeUC *analysisuc.UseCase) *PortfolioHandler {
	return &PortfolioHandler{uc: uc, analyzeUC: analyzeUC}
}

func (h *PortfolioHandler) RegisterRoutes(rg *gin.RouterGroup) {
	pf := rg.Group("/portfolios")
	pf.POST("", h.createPortfolio)
	pf.GET("", h.listPortfolios)
	pf.GET("/summary", h.getPortfoliosSummary)
	pf.GET("/history", h.getValueHistory)
	pf.GET("/benchmarks", h.listBenchmarks)
	pf.GET("/positions", h.listAllPositions)
	pf.GET("/activity", h.getRecentActivity)
	pf.POST("/analyze", h.analyze)
	pf.GET("/:id", h.getPortfolio)
	pf.PATCH("/:id", h.updatePortfolio)
	pf.DELETE("/:id", h.deletePortfolio)
	pf.POST("/:id/positions", h.addPosition)
	pf.GET("/:id/positions", h.listPositions)
	pf.PATCH("/:id/positions/:symbol", h.updatePosition)
	pf.DELETE("/:id/positions/:symbol", h.removePosition)
	pf.GET("/:id/transactions", h.listTransactions)
	pf.POST("/:id/transactions", h.createTransaction)
	pf.PATCH("/:id/transactions/:txId", h.updateTransaction)
	pf.DELETE("/:id/transactions/:txId", h.deleteTransaction)
	pf.GET("/:id/summary", h.getSummary)
	pf.GET("/:id/activity", h.getActivity)
	pf.POST("/:id/analyze", h.analyzePortfolio)
}

// --- Portfolios ---

type createPortfolioRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (h *PortfolioHandler) createPortfolio(c *gin.Context) {
	var req createPortfolioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.Err(c, http.StatusBadRequest, "name is required")
		return
	}

	email := c.GetString(middleware.EmailKey)
	p, err := h.uc.CreatePortfolio(c.Request.Context(), email, req.Name, req.Description)
	if errors.Is(err, portfoliodomain.ErrPortfolioLimitReached) {
		response.Err(c, http.StatusConflict, "portfolio limit reached")
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to create portfolio", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to create portfolio")
		return
	}
	response.Created(c, p)
}

func (h *PortfolioHandler) listPortfolios(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)
	portfolios, err := h.uc.ListPortfolios(c.Request.Context(), email)
	if err != nil {
		zap.L().Error(
			"failed to list portfolios", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to list portfolios")
		return
	}
	response.OK(c, portfolios)
}

func (h *PortfolioHandler) getPortfoliosSummary(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)
	summary, err := h.uc.GetPortfoliosSummary(c.Request.Context(), email)
	if err != nil {
		zap.L().Error(
			"failed to load portfolios summary", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to load portfolios summary")
		return
	}
	response.OK(c, summary)
}

type valueHistoryQuery struct {
	Range string `form:"range" binding:"omitempty,oneof=7D 30D 90D 1Y All"`
	// Benchmark is validated by hand against the configured list (it is not a fixed enum).
	Benchmark string `form:"benchmark"`
}

func (h *PortfolioHandler) listBenchmarks(c *gin.Context) {
	response.OK(c, h.uc.Benchmarks())
}

func (h *PortfolioHandler) getValueHistory(c *gin.Context) {
	var q valueHistoryQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Err(c, http.StatusBadRequest, "range must be one of: 7D, 30D, 90D, 1Y, All")
		return
	}
	r := portfoliodomain.HistoryRange(q.Range)
	if r == "" {
		r = portfoliodomain.HistoryRange30D
	}

	if q.Benchmark != "" && !h.knownBenchmark(q.Benchmark) {
		symbols := make([]string, 0)
		for _, b := range h.uc.Benchmarks() {
			symbols = append(symbols, b.Symbol)
		}
		response.Err(c, http.StatusBadRequest, "benchmark must be one of: "+strings.Join(symbols, ", "))
		return
	}

	email := c.GetString(middleware.EmailKey)
	history, err := h.uc.GetValueHistory(c.Request.Context(), email, r, q.Benchmark, time.Now())
	if err != nil {
		zap.L().Error(
			"failed to load value history", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.String("range", string(r)), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to load value history")
		return
	}
	response.OK(c, history)
}

// knownBenchmark reports whether symbol is a configured benchmark (case-sensitive).
func (h *PortfolioHandler) knownBenchmark(symbol string) bool {
	for _, b := range h.uc.Benchmarks() {
		if b.Symbol == symbol {
			return true
		}
	}
	return false
}

func (h *PortfolioHandler) listAllPositions(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)
	positions, err := h.uc.ListAllPositions(c.Request.Context(), email)
	if err != nil {
		zap.L().Error(
			"failed to list all positions", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to list positions")
		return
	}
	response.OK(c, positions)
}

type recentActivityQuery struct {
	Limit int `form:"limit" binding:"omitempty,min=1,max=50"`
}

func (h *PortfolioHandler) getRecentActivity(c *gin.Context) {
	var q recentActivityQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Err(c, http.StatusBadRequest, "limit must be between 1 and 50")
		return
	}
	if q.Limit == 0 {
		q.Limit = 10
	}

	email := c.GetString(middleware.EmailKey)
	activities, err := h.uc.GetRecentActivity(c.Request.Context(), email, q.Limit)
	if err != nil {
		zap.L().Error(
			"failed to fetch recent activity", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to fetch activity")
		return
	}
	response.OK(c, activities)
}

func (h *PortfolioHandler) getPortfolio(c *gin.Context) {
	id := c.Param("id")
	email := c.GetString(middleware.EmailKey)
	p, err := h.uc.GetPortfolio(c.Request.Context(), email, id)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to get portfolio", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.String("id", id), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to get portfolio")
		return
	}
	response.OK(c, p)
}

type updatePortfolioRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (h *PortfolioHandler) updatePortfolio(c *gin.Context) {
	id := c.Param("id")

	var req updatePortfolioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == nil && req.Description == nil {
		response.Err(c, http.StatusBadRequest, "at least one of name or description is required")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		response.Err(c, http.StatusBadRequest, "name must not be empty")
		return
	}

	email := c.GetString(middleware.EmailKey)
	p, err := h.uc.UpdatePortfolio(c.Request.Context(), email, id, req.Name, req.Description)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to update portfolio", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.String("id", id), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to update portfolio")
		return
	}
	response.OK(c, p)
}

func (h *PortfolioHandler) deletePortfolio(c *gin.Context) {
	id := c.Param("id")
	email := c.GetString(middleware.EmailKey)
	err := h.uc.DeletePortfolio(c.Request.Context(), email, id)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to delete portfolio", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.String("id", id), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to delete portfolio")
		return
	}
	c.Status(http.StatusNoContent)
}

// --- Positions ---

type addPositionRequest struct {
	Symbol  string   `json:"symbol"`
	Name    string   `json:"name"`
	Shares  *float64 `json:"shares"`
	AvgCost *float64 `json:"avgCost"`
}

func (h *PortfolioHandler) addPosition(c *gin.Context) {
	id := c.Param("id")

	var req addPositionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, "symbol, name, shares and avgCost are required")
		return
	}
	if req.Symbol == "" || req.Name == "" || req.Shares == nil || req.AvgCost == nil {
		response.Err(c, http.StatusBadRequest, "symbol, name, shares and avgCost are required")
		return
	}
	if *req.Shares <= 0 {
		response.Err(c, http.StatusBadRequest, "shares must be greater than 0")
		return
	}
	if *req.AvgCost < 0 {
		response.Err(c, http.StatusBadRequest, "avgCost must not be negative")
		return
	}

	email := c.GetString(middleware.EmailKey)
	pos, err := h.uc.AddPosition(c.Request.Context(), email, id, req.Symbol, req.Name, *req.Shares, *req.AvgCost)
	if errors.Is(err, portfoliodomain.ErrAlreadyExists) {
		response.Err(c, http.StatusConflict, fmt.Sprintf("position already exists: %s", req.Symbol))
		return
	}
	if err != nil {
		// Portfolio not found, position limit, 400 validation (invalid transaction, future
		// date) and the 500 fallback share the ledger endpoints' mapping.
		h.transactionError(c, err, "add position", zap.String("symbol", req.Symbol))
		return
	}
	response.Created(c, pos)
}

// updatePosition is gone: a holding is no longer edited directly, only through its
// transactions (see portfolio_transactions.go). The route stays for a release so a stale
// client gets a clear 410 instead of a 404.
func (h *PortfolioHandler) updatePosition(c *gin.Context) {
	id := c.Param("id")
	email := c.GetString(middleware.EmailKey)
	_, err := h.uc.UpdatePosition(c.Request.Context(), email, id, c.Param("symbol"))
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if errors.Is(err, portfoliouc.ErrHoldingEditGone) {
		response.Err(c, http.StatusGone, err.Error())
		return
	}
	zap.L().Error(
		"unexpected update position result",
		logger.RequestID(c.Request.Context()), zap.String("email", email), zap.String("id", id), zap.Error(err),
	)
	response.Err(c, http.StatusInternalServerError, "failed to update position")
}

func (h *PortfolioHandler) removePosition(c *gin.Context) {
	id := c.Param("id")
	symbol := c.Param("symbol")
	email := c.GetString(middleware.EmailKey)

	err := h.uc.RemovePosition(c.Request.Context(), email, id, symbol)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if errors.Is(err, portfoliodomain.ErrNotFound) {
		response.Err(c, http.StatusNotFound, fmt.Sprintf("position not found: %s", symbol))
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to remove position",
			logger.RequestID(c.Request.Context()), zap.String("email", email), zap.String("id", id),
			zap.String("symbol", symbol), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to remove position")
		return
	}
	c.Status(http.StatusNoContent)
}

type listPositionsQuery struct {
	IncludeClosed bool `form:"includeClosed"`
}

func (h *PortfolioHandler) listPositions(c *gin.Context) {
	id := c.Param("id")
	var q listPositionsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Err(c, http.StatusBadRequest, "includeClosed must be true or false")
		return
	}
	email := c.GetString(middleware.EmailKey)
	positions, err := h.uc.ListPositions(c.Request.Context(), email, id, q.IncludeClosed)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to list positions", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.String("id", id), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to list positions")
		return
	}
	response.OK(c, positions)
}

func (h *PortfolioHandler) getSummary(c *gin.Context) {
	id := c.Param("id")
	email := c.GetString(middleware.EmailKey)
	summary, err := h.uc.GetSummary(c.Request.Context(), email, id)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to load portfolio", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.String("id", id), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to load portfolio")
		return
	}
	response.OK(c, summary)
}

func (h *PortfolioHandler) getActivity(c *gin.Context) {
	id := c.Param("id")

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 50 {
		response.Err(c, http.StatusBadRequest, "limit must be between 1 and 50")
		return
	}

	email := c.GetString(middleware.EmailKey)
	activities, err := h.uc.GetActivity(c.Request.Context(), email, id, limit)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to fetch activity", logger.RequestID(c.Request.Context()), zap.String("email", email),
			zap.String("id", id), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to fetch activity")
		return
	}
	response.OK(c, activities)
}

// --- AI analysis (stateless) ---

type analyzeRequest struct {
	Portfolio struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
		Holdings    []struct {
			Ticker string  `json:"ticker" binding:"required"`
			Actual float64 `json:"actual" binding:"gte=0"`
			Target float64 `json:"target" binding:"gte=0"`
		} `json:"holdings" binding:"required,min=1,dive"`
	} `json:"portfolio"`
	Dimensions []string `json:"dimensions" binding:"required,min=1,dive,required"`
}

// analyze streams an AI-generated portfolio review as Server-Sent Events. It is an
// explicit exception to the standard response envelope (see AGENTS.md): only the
// pre-stream error paths (400/429/502) use response.Err; the success path writes
// text/event-stream and terminates with a single `data: [DONE]`.
func (h *PortfolioHandler) analyze(c *gin.Context) {
	var req analyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}

	holdings := make([]analysisuc.Holding, len(req.Portfolio.Holdings))
	for i, h := range req.Portfolio.Holdings {
		holdings[i] = analysisuc.Holding{Ticker: h.Ticker, Actual: h.Actual, Target: h.Target}
	}

	body, err := h.analyzeUC.Stream(
		c.Request.Context(), analysisuc.Input{
			Name:        req.Portfolio.Name,
			Description: req.Portfolio.Description,
			Holdings:    holdings,
			Dimensions:  req.Dimensions,
		},
	)
	if errors.Is(err, analysisdomain.ErrRateLimited) {
		zap.L().Warn("analysis rate limited", logger.RequestID(c.Request.Context()))
		response.Err(c, http.StatusTooManyRequests, "analysis service is rate limited, try again shortly")
		return
	}
	if err != nil {
		zap.L().Error("failed to generate analysis", logger.RequestID(c.Request.Context()), zap.Error(err))
		response.Err(c, http.StatusBadGateway, "failed to generate analysis")
		return
	}
	defer body.Close()

	h.streamAnalysis(c, body)
}

// analyzePortfolioRequest is the body for the per-portfolio analyze endpoint. Unlike the
// stateless /analyze, the holdings come from the stored portfolio; only the dimensions to
// analyze across are supplied by the client.
type analyzePortfolioRequest struct {
	Dimensions []string `json:"dimensions" binding:"required,min=1,dive,required"`
}

// analyzePortfolio streams an AI-generated review of a stored portfolio (identified by
// :id) as Server-Sent Events. Holdings and their actual weights are derived server-side
// from the portfolio's current market-value allocation; target weights equal actual, so
// the prompt sees zero drift. Like analyze, it is an exception to the standard response
// envelope: only the pre-stream error paths (400/404/429/502) use response.Err.
func (h *PortfolioHandler) analyzePortfolio(c *gin.Context) {
	var req analyzePortfolioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}

	id := c.Param("id")
	email := c.GetString(middleware.EmailKey)
	data, err := h.uc.BuildAnalysisData(c.Request.Context(), email, id)
	if errors.Is(err, portfoliodomain.ErrPortfolioNotFound) {
		response.Err(c, http.StatusNotFound, "portfolio not found")
		return
	}
	if errors.Is(err, portfoliodomain.ErrPortfolioEmpty) {
		response.Err(c, http.StatusBadRequest, "portfolio has no holdings to analyze")
		return
	}
	if errors.Is(err, portfoliodomain.ErrPricingUnavailable) {
		zap.L().Warn("analysis pricing unavailable", logger.RequestID(c.Request.Context()), zap.String("id", id))
		response.Err(c, http.StatusBadGateway, "pricing data unavailable, try again shortly")
		return
	}
	if err != nil {
		zap.L().Error(
			"failed to load portfolio for analysis",
			logger.RequestID(c.Request.Context()), zap.String("email", email), zap.String("id", id), zap.Error(err),
		)
		response.Err(c, http.StatusInternalServerError, "failed to load portfolio")
		return
	}

	holdings := make([]analysisuc.Holding, len(data.Holdings))
	for i, hld := range data.Holdings {
		// Stored portfolios carry no target allocation; target = actual (zero drift).
		holdings[i] = analysisuc.Holding{Ticker: hld.Ticker, Actual: hld.Weight, Target: hld.Weight}
	}

	body, err := h.analyzeUC.Stream(
		c.Request.Context(), analysisuc.Input{
			Name:        data.Name,
			Description: data.Description,
			Holdings:    holdings,
			Dimensions:  req.Dimensions,
		},
	)
	if errors.Is(err, analysisdomain.ErrRateLimited) {
		zap.L().Warn("analysis rate limited", logger.RequestID(c.Request.Context()))
		response.Err(c, http.StatusTooManyRequests, "analysis service is rate limited, try again shortly")
		return
	}
	if err != nil {
		zap.L().Error("failed to generate analysis", logger.RequestID(c.Request.Context()), zap.Error(err))
		response.Err(c, http.StatusBadGateway, "failed to generate analysis")
		return
	}
	defer body.Close()

	h.streamAnalysis(c, body)
}

// streamAnalysis forwards an upstream SSE body to the client as text/event-stream,
// flushing each line so nothing buffers. The upstream's own `data: [DONE]` terminal is
// dropped and exactly one is emitted at the end. Shared by the analyze endpoints; it is
// an explicit exception to the standard response envelope (see AGENTS.md).
func (h *PortfolioHandler) streamAnalysis(c *gin.Context, body io.Reader) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	// No "Connection: keep-alive" header: it is hop-by-hop and illegal over HTTP/2, and
	// redundant on HTTP/1.1 since Go keeps the connection alive without it.
	c.Status(http.StatusOK)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// [DONE] is always the terminal frame; stop here and emit our own below.
		if strings.TrimSpace(line) == "data: [DONE]" {
			break
		}
		fmt.Fprintf(c.Writer, "%s\n", line)
		c.Writer.Flush()
	}
	if err := scanner.Err(); err != nil {
		zap.L().Error("analysis stream truncated", logger.RequestID(c.Request.Context()), zap.Error(err))
	}

	fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
