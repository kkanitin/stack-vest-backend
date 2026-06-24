package handler

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	analysisdomain "github.com/kanitin/stackvest/backend/internal/domain/analysis"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	analysisuc "github.com/kanitin/stackvest/backend/internal/usecase/analysis"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
)

type PortfolioHandler struct {
	uc        *portfoliouc.UseCase
	analyzeUC *analysisuc.UseCase
}

func NewPortfolioHandler(uc *portfoliouc.UseCase, analyzeUC *analysisuc.UseCase) *PortfolioHandler {
	return &PortfolioHandler{uc: uc, analyzeUC: analyzeUC}
}

func (h *PortfolioHandler) RegisterRoutes(rg *gin.RouterGroup) {
	pf := rg.Group("/portfolio")
	pf.POST("/positions", h.addPosition)
	pf.DELETE("/positions/:symbol", h.removePosition)
	pf.PATCH("/positions/:symbol", h.updatePosition)
	pf.GET("/positions", h.listPositions)
	pf.GET("/summary", h.getSummary)
	pf.GET("/activity", h.getActivity)
	pf.POST("/analyze", h.analyze)
}

type addPositionRequest struct {
	Symbol  string   `json:"symbol"`
	Name    string   `json:"name"`
	Shares  *float64 `json:"shares"`
	AvgCost *float64 `json:"avgCost"`
}

func (h *PortfolioHandler) addPosition(c *gin.Context) {
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
	pos, err := h.uc.AddPosition(c.Request.Context(), email, req.Symbol, req.Name, *req.Shares, *req.AvgCost)
	if errors.Is(err, portfoliodomain.ErrAlreadyExists) {
		response.Err(c, http.StatusConflict, fmt.Sprintf("position already exists: %s", req.Symbol))
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to add position", "email", email, "symbol", req.Symbol, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to add position")
		return
	}
	response.Created(c, pos)
}

type updatePositionRequest struct {
	Shares  *float64 `json:"shares"`
	AvgCost *float64 `json:"avgCost"`
}

func (h *PortfolioHandler) updatePosition(c *gin.Context) {
	symbol := c.Param("symbol")

	var req updatePositionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Shares == nil && req.AvgCost == nil {
		response.Err(c, http.StatusBadRequest, "at least one of shares or avgCost is required")
		return
	}
	if req.Shares != nil && *req.Shares <= 0 {
		response.Err(c, http.StatusBadRequest, "shares must be greater than 0")
		return
	}
	if req.AvgCost != nil && *req.AvgCost < 0 {
		response.Err(c, http.StatusBadRequest, "avgCost must not be negative")
		return
	}

	email := c.GetString(middleware.EmailKey)
	pos, err := h.uc.UpdatePosition(c.Request.Context(), email, symbol, req.Shares, req.AvgCost)
	if errors.Is(err, portfoliodomain.ErrNotFound) {
		response.Err(c, http.StatusNotFound, fmt.Sprintf("position not found: %s", symbol))
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to update position", "email", email, "symbol", symbol, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to update position")
		return
	}
	response.OK(c, pos)
}

func (h *PortfolioHandler) removePosition(c *gin.Context) {
	symbol := c.Param("symbol")
	email := c.GetString(middleware.EmailKey)

	err := h.uc.RemovePosition(c.Request.Context(), email, symbol)
	if errors.Is(err, portfoliodomain.ErrNotFound) {
		response.Err(c, http.StatusNotFound, fmt.Sprintf("position not found: %s", symbol))
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to remove position", "email", email, "symbol", symbol, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to remove position")
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *PortfolioHandler) listPositions(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)
	positions, err := h.uc.ListPositions(c.Request.Context(), email)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to list positions", "email", email, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to list positions")
		return
	}
	response.OK(c, positions)
}

func (h *PortfolioHandler) getSummary(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)
	summary, err := h.uc.GetSummary(c.Request.Context(), email)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to load portfolio", "email", email, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to load portfolio")
		return
	}
	response.OK(c, summary)
}

func (h *PortfolioHandler) getActivity(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 50 {
		response.Err(c, http.StatusBadRequest, "limit must be between 1 and 50")
		return
	}

	email := c.GetString(middleware.EmailKey)
	activities, err := h.uc.GetActivity(c.Request.Context(), email, limit)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to fetch activity", "email", email, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to fetch activity")
		return
	}
	response.OK(c, activities)
}

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

	body, err := h.analyzeUC.Stream(c.Request.Context(), analysisuc.Input{
		Name:        req.Portfolio.Name,
		Description: req.Portfolio.Description,
		Holdings:    holdings,
		Dimensions:  req.Dimensions,
	})
	if errors.Is(err, analysisdomain.ErrRateLimited) {
		slog.WarnContext(c.Request.Context(), "analysis rate limited")
		response.Err(c, http.StatusTooManyRequests, "analysis service is rate limited, try again shortly")
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to generate analysis", "error", err)
		response.Err(c, http.StatusBadGateway, "failed to generate analysis")
		return
	}
	defer body.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	// Forward each upstream SSE line as it arrives, flushing so nothing buffers.
	// Drop the upstream's own `data: [DONE]` terminal; we emit exactly one below.
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
		slog.ErrorContext(c.Request.Context(), "analysis stream truncated", "error", err)
	}

	fmt.Fprint(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
