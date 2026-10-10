package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	"github.com/kanitin/stackvest/backend/internal/domain/dca"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
	"github.com/kanitin/stackvest/backend/pkg/logger"
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

// compareDCARequest allows at most three symbols (the max=3 tag).
type compareDCARequest struct {
	Symbols   []string `json:"symbols"   binding:"required,min=1,max=3,dive,required"`
	StartDate string   `json:"startDate" binding:"required"`
	EndDate   string   `json:"endDate"   binding:"required"`
	Amount    float64  `json:"amount"    binding:"required,gt=0"`
	Frequency string   `json:"frequency" binding:"required"`
}

type holdingRequest struct {
	Symbol string  `json:"symbol" binding:"required"`
	Weight float64 `json:"weight" binding:"required,gt=0"`
}

// holdingsDCARequest allows at most 20 holdings, the default per-portfolio position limit
// (portfolio.max_positions_per_portfolio).
type holdingsDCARequest struct {
	Holdings  []holdingRequest `json:"holdings"  binding:"required,min=1,max=20,dive"`
	StartDate string           `json:"startDate" binding:"required"`
	EndDate   string           `json:"endDate"   binding:"required"`
	Amount    float64          `json:"amount"    binding:"required,gt=0"`
	Frequency string           `json:"frequency" binding:"required"`
}

// parsePlan validates the date range and frequency shared by every DCA request. On failure it
// returns the message to send with a 400. Symbol and Amount are left for the caller.
func parsePlan(startStr, endStr, freqStr string) (dca.SimulationInput, string) {
	startDate, err := time.Parse("2006-01-02", startStr)
	if err != nil {
		return dca.SimulationInput{}, "startDate must be in YYYY-MM-DD format"
	}
	endDate, err := time.Parse("2006-01-02", endStr)
	if err != nil {
		return dca.SimulationInput{}, "endDate must be in YYYY-MM-DD format"
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if endDate.After(today) {
		return dca.SimulationInput{}, "endDate cannot be in the future"
	}
	if !startDate.Before(endDate) {
		return dca.SimulationInput{}, "startDate must be before endDate"
	}
	freq := dca.Frequency(freqStr)
	if !freq.IsValid() {
		return dca.SimulationInput{}, "frequency must be one of: daily, weekly, biweekly, monthly"
	}
	maxYears := maxDateRangeYears[freq]
	if endDate.After(startDate.AddDate(maxYears, 0, 0)) {
		return dca.SimulationInput{}, fmt.Sprintf("date range exceeds the maximum allowed for the selected frequency (%d years)", maxYears)
	}
	return dca.SimulationInput{StartDate: startDate, EndDate: endDate, Frequency: freq}, ""
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
	dca.POST("/compare", h.compare)
	dca.POST("/holdings", h.holdings)
}

func (h *DCAHandler) simulate(c *gin.Context) {
	var req simulateDCARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}

	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))

	plan, errMsg := parsePlan(req.StartDate, req.EndDate, req.Frequency)
	if errMsg != "" {
		response.Err(c, http.StatusBadRequest, errMsg)
		return
	}

	plan.Symbol = symbol
	plan.Amount = req.Amount
	result, err := h.simulatorUC.Execute(plan)
	if errors.Is(err, dca.ErrSymbolNotFound) {
		response.Err(c, http.StatusNotFound, fmt.Sprintf("symbol not found: %s", symbol))
		return
	}
	if errors.Is(err, dca.ErrDateRangeTooShort) {
		response.Err(c, http.StatusBadRequest, "date range too short for the selected frequency")
		return
	}
	if err != nil {
		zap.L().Error("dca simulation failed", logger.RequestID(c.Request.Context()), zap.String("symbol", symbol), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to fetch historical prices")
		return
	}

	response.OK(c, result)
}

// compare runs one plan over up to three symbols in a single request, so a comparison costs
// one call against the per-user request limit rather than one per asset.
func (h *DCAHandler) compare(c *gin.Context) {
	var req compareDCARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}

	plan, errMsg := parsePlan(req.StartDate, req.EndDate, req.Frequency)
	if errMsg != "" {
		response.Err(c, http.StatusBadRequest, errMsg)
		return
	}
	plan.Amount = req.Amount

	seen := make(map[string]bool, len(req.Symbols))
	symbols := make([]string, 0, len(req.Symbols))
	for _, raw := range req.Symbols {
		symbol := strings.ToUpper(strings.TrimSpace(raw))
		if symbol == "" {
			response.Err(c, http.StatusBadRequest, "symbols must not be empty")
			return
		}
		if !seen[symbol] {
			seen[symbol] = true
			symbols = append(symbols, symbol)
		}
	}

	cmp, err := h.simulatorUC.Compare(symbols, plan)
	if err != nil {
		zap.L().Error("dca comparison failed", logger.RequestID(c.Request.Context()), zap.Strings("symbols", symbols), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to fetch historical prices")
		return
	}
	response.OK(c, cmp)
}

// holdings runs one plan across a portfolio's holdings at the given weights, in a single request
// however many holdings there are. Each purchase is split by weight; it is hypothetical.
func (h *DCAHandler) holdings(c *gin.Context) {
	var req holdingsDCARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return
	}

	plan, errMsg := parsePlan(req.StartDate, req.EndDate, req.Frequency)
	if errMsg != "" {
		response.Err(c, http.StatusBadRequest, errMsg)
		return
	}
	plan.Amount = req.Amount

	// The same symbol listed twice is one holding with the weights added.
	index := make(map[string]int, len(req.Holdings))
	holdings := make([]dca.Holding, 0, len(req.Holdings))
	for _, raw := range req.Holdings {
		symbol := strings.ToUpper(strings.TrimSpace(raw.Symbol))
		if symbol == "" {
			response.Err(c, http.StatusBadRequest, "holdings must not have a blank symbol")
			return
		}
		if i, ok := index[symbol]; ok {
			holdings[i].Weight += raw.Weight
			continue
		}
		index[symbol] = len(holdings)
		holdings = append(holdings, dca.Holding{Symbol: symbol, Weight: raw.Weight})
	}

	sim, err := h.simulatorUC.SimulateHoldings(holdings, plan)
	if err != nil {
		zap.L().Error("dca holdings simulation failed", logger.RequestID(c.Request.Context()), zap.Int("holdings", len(holdings)), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to fetch historical prices")
		return
	}
	response.OK(c, sim)
}
