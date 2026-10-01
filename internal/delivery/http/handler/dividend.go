package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	dividenddomain "github.com/kanitin/stackvest/backend/internal/domain/dividend"
	"github.com/kanitin/stackvest/backend/pkg/logger"
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

// calendarRangeErrors are the use case's range-validation sentinels. Each maps to a
// 400 whose text is the sentinel's own message.
var calendarRangeErrors = []error{
	dividenddomain.ErrInvalidRange,
	dividenddomain.ErrRangeTooLong,
	dividenddomain.ErrRangeOutOfBounds,
}

// getCalendar returns the dividends of the authenticated user's holdings whose
// reference date falls in a date range. Optional `from`/`to` query params
// (YYYY-MM-DD) are honored as sent, past dates included: `from` defaults to today
// and `to` to `from` + 75 days. Nothing is clamped; a range the use case rejects is
// a 400: `to` before `from`, a span over 92 days, or dates more than 13 months
// before or after the current month. Results are paginated via the standard
// `page`/`size` query params.
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
		response.Err(c, http.StatusBadRequest, dividenddomain.ErrInvalidRange.Error())
		return
	}

	page, size, ok := parsePagination(c)
	if !ok {
		return
	}

	email := c.GetString(middleware.EmailKey)
	entries, err := h.calendarUC.Execute(c.Request.Context(), email, from, to)
	for _, rangeErr := range calendarRangeErrors {
		if errors.Is(err, rangeErr) {
			response.Err(c, http.StatusBadRequest, rangeErr.Error())
			return
		}
	}
	if err != nil {
		zap.L().Error("failed to build dividend calendar", logger.RequestID(c.Request.Context()), zap.String("email", email), zap.Error(err))
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
