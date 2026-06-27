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

const dateLayout = "2006-01-02"

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
// than an error.
func (h *DividendHandler) getCalendar(c *gin.Context) {
	var from, to time.Time
	if v := c.Query("from"); v != "" {
		t, err := time.Parse(dateLayout, v)
		if err != nil {
			response.Err(c, http.StatusBadRequest, "from must be a date in YYYY-MM-DD format")
			return
		}
		from = t
	}
	if v := c.Query("to"); v != "" {
		t, err := time.Parse(dateLayout, v)
		if err != nil {
			response.Err(c, http.StatusBadRequest, "to must be a date in YYYY-MM-DD format")
			return
		}
		to = t
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		response.Err(c, http.StatusBadRequest, "to must not be before from")
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
	response.OKList(c, entries, response.Meta{
		Total:            &total,
		CurrentPageCount: &total,
	})
}
