package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	watchlistdomain "github.com/kanitin/stackvest/backend/internal/domain/watchlist"
	watchlistuc "github.com/kanitin/stackvest/backend/internal/usecase/watchlist"
)

type WatchlistHandler struct {
	watchlistUC *watchlistuc.WatchlistUseCase
}

func NewWatchlistHandler(watchlistUC *watchlistuc.WatchlistUseCase) *WatchlistHandler {
	return &WatchlistHandler{watchlistUC: watchlistUC}
}

func (h *WatchlistHandler) RegisterRoutes(rg *gin.RouterGroup) {
	wl := rg.Group("/watchlist")
	wl.GET("", h.list)
	wl.POST("", h.add)
	wl.DELETE("/:symbol", h.remove)
	wl.PATCH("/:symbol/alerts", h.setAlerts)
}

type addItemRequest struct {
	Symbol   string   `json:"symbol" binding:"required"`
	Name     string   `json:"name" binding:"required"`
	Type     string   `json:"type"`
	Category []string `json:"category"`
}

func (h *WatchlistHandler) list(c *gin.Context) {
	page, size, ok := parsePagination(c)
	if !ok {
		return
	}

	email := c.GetString(middleware.EmailKey)

	items, err := h.watchlistUC.List(c.Request.Context(), email)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to list watchlist", "email", email, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to list watchlist")
		return
	}

	total := len(items)
	offset := (page - 1) * size
	if offset > total {
		offset = total
	}
	end := offset + size
	if end > total {
		end = total
	}
	pageItems := items[offset:end]
	currentPageCount := len(pageItems)

	response.OKList(
		c, pageItems, response.Meta{
			Total:            &total,
			Page:             &page,
			Size:             &size,
			CurrentPageCount: &currentPageCount,
		},
	)
}

func (h *WatchlistHandler) add(c *gin.Context) {
	var req addItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, "symbol and name are required")
		return
	}

	email := c.GetString(middleware.EmailKey)

	item, err := h.watchlistUC.Add(c.Request.Context(), email, req.Symbol, req.Name, req.Type, req.Category)
	if errors.Is(err, watchlistdomain.ErrInvalidSymbol) {
		response.Err(c, http.StatusBadRequest, "invalid symbol")
		return
	}
	if errors.Is(err, watchlistdomain.ErrAlreadyExists) {
		response.Err(c, http.StatusConflict, "symbol already in watchlist")
		return
	}
	if err != nil {
		slog.ErrorContext(
			c.Request.Context(), "failed to add watchlist item", "email", email, "symbol", req.Symbol, "error", err,
		)
		response.Err(c, http.StatusInternalServerError, "failed to add watchlist item")
		return
	}

	response.Created(c, item)
}

type setAlertsRequest struct {
	Enabled *bool `json:"enabled" binding:"required"`
}

type alertsResponse struct {
	Symbol        string `json:"symbol"`
	AlertsEnabled bool   `json:"alertsEnabled"`
}

func (h *WatchlistHandler) setAlerts(c *gin.Context) {
	symbol := c.Param("symbol")
	email := c.GetString(middleware.EmailKey)

	var req setAlertsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, "enabled must be a boolean")
		return
	}

	item, err := h.watchlistUC.SetAlerts(c.Request.Context(), email, symbol, *req.Enabled)
	if errors.Is(err, watchlistdomain.ErrNotFound) {
		response.Err(c, http.StatusNotFound, "symbol not in watchlist")
		return
	}
	if err != nil {
		slog.ErrorContext(
			c.Request.Context(), "failed to update alerts", "email", email, "symbol", symbol, "error", err,
		)
		response.Err(c, http.StatusInternalServerError, "failed to update alerts")
		return
	}

	response.OK(c, alertsResponse{Symbol: item.Symbol, AlertsEnabled: item.AlertsEnabled})
}

func (h *WatchlistHandler) remove(c *gin.Context) {
	symbol := c.Param("symbol")
	email := c.GetString(middleware.EmailKey)

	err := h.watchlistUC.Remove(c.Request.Context(), email, symbol)
	if errors.Is(err, watchlistdomain.ErrNotFound) {
		response.Err(c, http.StatusNotFound, "symbol not in watchlist")
		return
	}
	if err != nil {
		slog.ErrorContext(
			c.Request.Context(), "failed to remove watchlist item", "email", email, "symbol", symbol, "error", err,
		)
		response.Err(c, http.StatusInternalServerError, "failed to remove watchlist item")
		return
	}

	c.Status(http.StatusNoContent)
}
