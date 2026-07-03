package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	dividenddomain "github.com/kanitin/stackvest/backend/internal/domain/dividend"
)

type dividendCalendarUseCase interface {
	Execute(ctx context.Context, email string, from, to time.Time) ([]dividenddomain.CalendarEntry, error)
}

type DividendHandler struct {
	calendarUC dividendCalendarUseCase
}

func NewDividendHandler(calendarUC dividendCalendarUseCase) *DividendHandler {
	return &DividendHandler{calendarUC: calendarUC}
}

func (h *DividendHandler) RegisterRoutes(rg *gin.RouterGroup) {
	d := rg.Group("/dividends")
	d.GET("/calendar", h.getCalendar)
}

// getCalendar returns upcoming dividends for the authenticated user's holdings.
// Optional `from`/`to` query params (YYYY-MM-DD) narrow the view, but only within
// the fixed forward window the use case fetches (~today → +75 days): values outside
// it are clamped, so a request beyond the window returns the available subset rather
// than an error. Results are paginated via the standard `page`/`size` query params.
func (h *DividendHandler) getCalendar(c *gin.Context) {
	from, ok := parseQueryDate(c, "from")
	if !ok {
		return
	}
	to, ok := parseQueryDate(c, "to")
	if !ok {
		return
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		response.Err(c, http.StatusBadRequest, "to must not be before from")
		return
	}

	page, size, ok := parsePagination(c)
	if !ok {
		return
	}

	email := c.GetString(middleware.EmailKey)
	entries, err := h.calendarUC.Execute(c.Request.Context(), email, from, to)
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to build dividend calendar", "email", email, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to build dividend calendar")
		return
	}

	total := len(entries)
	offset := (page - 1) * size
	if offset > total {
		offset = total
	}
	end := offset + size
	if end > total {
		end = total
	}
	pageEntries := entries[offset:end]
	currentPageCount := len(pageEntries)

	response.OKList(c, pageEntries, response.Meta{
		Total:            &total,
		Page:             &page,
		Size:             &size,
		CurrentPageCount: &currentPageCount,
	})
}
