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
}

type addItemRequest struct {
	Symbol string `json:"symbol" binding:"required"`
	Name   string `json:"name" binding:"required"`
	Type   string `json:"type"`
}

func (h *WatchlistHandler) list(c *gin.Context) {
	email := c.GetString(middleware.EmailKey)

	items, total, err := h.watchlistUC.List(c.Request.Context(), email)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to list watchlist", "email", email, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to list watchlist")
		return
	}

	currentPageCount := len(items)
	response.OKList(
		c, items, response.Meta{
			Total:            &total,
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

	item, err := h.watchlistUC.Add(c.Request.Context(), email, req.Symbol, req.Name, req.Type)
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
