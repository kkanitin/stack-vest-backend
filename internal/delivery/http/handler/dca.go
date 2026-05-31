package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	"github.com/kanitin/stackvest/backend/internal/domain/dca"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
)

var maxDateRangeYears = map[dca.Frequency]int{
	dca.FrequencyDaily:    5,
	dca.FrequencyWeekly:   15,
	dca.FrequencyBiweekly: 20,
	dca.FrequencyMonthly:  30,
}

type simulateDCARequest struct {
	Symbol    string  `json:"symbol"    binding:"required"`
	StartDate string  `json:"startDate" binding:"required"`
	EndDate   string  `json:"endDate"   binding:"required"`
	Amount    float64 `json:"amount"    binding:"required,gt=0"`
	Frequency string  `json:"frequency" binding:"required"`
}

type DCAHandler struct {
	simulatorUC *dcauc.SimulatorUseCase
}

func NewDCAHandler(simulatorUC *dcauc.SimulatorUseCase) *DCAHandler {
	return &DCAHandler{simulatorUC: simulatorUC}
}

func (h *DCAHandler) RegisterRoutes(rg *gin.RouterGroup) {
	dca := rg.Group("/dca")
	dca.POST("/simulate", h.simulate)
}

func (h *DCAHandler) simulate(c *gin.Context) {
	var req simulateDCARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}

	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))

	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		response.Err(c, http.StatusBadRequest, "startDate must be in YYYY-MM-DD format")
		return
	}

	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		response.Err(c, http.StatusBadRequest, "endDate must be in YYYY-MM-DD format")
		return
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	if endDate.After(today) {
		response.Err(c, http.StatusBadRequest, "endDate cannot be in the future")
		return
	}

	if !startDate.Before(endDate) {
		response.Err(c, http.StatusBadRequest, "startDate must be before endDate")
		return
	}

	freq := dca.Frequency(req.Frequency)
	if !freq.IsValid() {
		response.Err(c, http.StatusBadRequest, "frequency must be one of: daily, weekly, biweekly, monthly")
		return
	}

	maxYears := maxDateRangeYears[freq]
	if endDate.After(startDate.AddDate(maxYears, 0, 0)) {
		response.Err(c, http.StatusBadRequest,
			fmt.Sprintf("date range exceeds the maximum allowed for the selected frequency (%d years)", maxYears))
		return
	}

	result, err := h.simulatorUC.Execute(dca.SimulationInput{
		Symbol:    symbol,
		StartDate: startDate,
		EndDate:   endDate,
		Amount:    req.Amount,
		Frequency: freq,
	})
	if errors.Is(err, dca.ErrSymbolNotFound) {
		response.Err(c, http.StatusNotFound, fmt.Sprintf("symbol not found: %s", symbol))
		return
	}
	if errors.Is(err, dca.ErrDateRangeTooShort) {
		response.Err(c, http.StatusBadRequest, "date range too short for the selected frequency")
		return
	}
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "dca simulation failed", "symbol", symbol, "error", err)
		response.Err(c, http.StatusInternalServerError, "failed to fetch historical prices")
		return
	}

	response.OK(c, result)
}
