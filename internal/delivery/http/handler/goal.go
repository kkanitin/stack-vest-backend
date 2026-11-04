package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
	goaldomain "github.com/kanitin/stackvest/backend/internal/domain/goal"
	goaluc "github.com/kanitin/stackvest/backend/internal/usecase/goal"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

type GoalHandler struct {
	uc *goaluc.UseCase
}

func NewGoalHandler(uc *goaluc.UseCase) *GoalHandler {
	return &GoalHandler{uc: uc}
}

func (h *GoalHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/goals")
	g.GET("", h.list)
	g.POST("", h.create)
	g.GET("/:id", h.get)
	g.PUT("/:id", h.update)
	g.DELETE("/:id", h.delete)
}

// goalRequest is the body of both POST and PUT (PUT replaces every field).
// Numbers that may legitimately be 0 are pointers so "missing" and "0" differ.
type goalRequest struct {
	Name               string   `json:"name" binding:"required,max=100"`
	Kind               string   `json:"kind" binding:"required,oneof=projection target"`
	Currency           string   `json:"currency" binding:"omitempty,max=10"`
	StartingAmount     *float64 `json:"startingAmount" binding:"omitempty,gte=0"`
	MonthlyAmount      *float64 `json:"monthlyAmount" binding:"required_if=Kind projection,omitempty,gte=0"`
	TargetAmount       *float64 `json:"targetAmount" binding:"required_if=Kind target,omitempty,gt=0"`
	Years              int      `json:"years" binding:"required,gt=0,lte=100"`
	ExpectedReturn     *float64 `json:"expectedReturn" binding:"required,gte=-100,lte=100"`
	ContributionGrowth *float64 `json:"contributionGrowth" binding:"omitempty,gte=-100,lte=100"`
	Inflation          *float64 `json:"inflation" binding:"omitempty,gte=-100,lte=100"`
	PortfolioID        *string  `json:"portfolioId" binding:"omitempty,min=1"`
}

func (r *goalRequest) toGoal() *goaldomain.Goal {
	g := &goaldomain.Goal{
		PortfolioID:        r.PortfolioID,
		Name:               strings.TrimSpace(r.Name),
		Kind:               goaldomain.Kind(r.Kind),
		Currency:           strings.ToUpper(strings.TrimSpace(r.Currency)),
		MonthlyAmount:      r.MonthlyAmount,
		TargetAmount:       r.TargetAmount,
		Years:              r.Years,
		ExpectedReturn:     *r.ExpectedReturn,
		ContributionGrowth: r.ContributionGrowth,
		Inflation:          r.Inflation,
	}
	if r.StartingAmount != nil {
		g.StartingAmount = *r.StartingAmount
	}
	return g
}

// bindGoal binds and validates the body, writing a 400 and returning nil on failure.
func bindGoal(c *gin.Context) *goaldomain.Goal {
	var req goalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Err(c, http.StatusBadRequest, err.Error())
		return nil
	}
	// `required` accepts a whitespace-only name, which would be stored as "".
	if strings.TrimSpace(req.Name) == "" {
		response.Err(c, http.StatusBadRequest, "name must not be empty")
		return nil
	}
	return req.toGoal()
}

func (h *GoalHandler) list(c *gin.Context) {
	page, size, ok := parsePagination(c)
	if !ok {
		return
	}
	email := c.GetString(middleware.EmailKey)
	goals, total, err := h.uc.List(c.Request.Context(), email, size, (page-1)*size)
	if err != nil {
		zap.L().Error("failed to list goals", logger.RequestID(c.Request.Context()), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to list goals")
		return
	}
	count := len(goals)
	response.OKList(c, goals, response.Meta{Total: &total, Page: &page, Size: &size, CurrentPageCount: &count})
}

func (h *GoalHandler) create(c *gin.Context) {
	g := bindGoal(c)
	if g == nil {
		return
	}
	email := c.GetString(middleware.EmailKey)
	created, err := h.uc.Create(c.Request.Context(), email, g)
	switch {
	case errors.Is(err, goaldomain.ErrInvalidPortfolio):
		response.Err(c, http.StatusBadRequest, "portfolioId not found")
	case errors.Is(err, goaldomain.ErrGoalLimitReached):
		response.Err(c, http.StatusConflict, "goal limit reached")
	case err != nil:
		zap.L().Error("failed to create goal", logger.RequestID(c.Request.Context()), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to create goal")
	default:
		response.Created(c, created)
	}
}

func (h *GoalHandler) get(c *gin.Context) {
	id := c.Param("id")
	email := c.GetString(middleware.EmailKey)
	g, err := h.uc.Get(c.Request.Context(), email, id)
	switch {
	case errors.Is(err, goaldomain.ErrGoalNotFound):
		response.Err(c, http.StatusNotFound, "goal not found")
	case err != nil:
		zap.L().Error("failed to get goal", logger.RequestID(c.Request.Context()), zap.String("id", id), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to get goal")
	default:
		response.OK(c, g)
	}
}

func (h *GoalHandler) update(c *gin.Context) {
	id := c.Param("id")
	g := bindGoal(c)
	if g == nil {
		return
	}
	email := c.GetString(middleware.EmailKey)
	updated, err := h.uc.Update(c.Request.Context(), email, id, g)
	switch {
	case errors.Is(err, goaldomain.ErrGoalNotFound):
		response.Err(c, http.StatusNotFound, "goal not found")
	case errors.Is(err, goaldomain.ErrInvalidPortfolio):
		response.Err(c, http.StatusBadRequest, "portfolioId not found")
	case err != nil:
		zap.L().Error("failed to update goal", logger.RequestID(c.Request.Context()), zap.String("id", id), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to update goal")
	default:
		response.OK(c, updated)
	}
}

func (h *GoalHandler) delete(c *gin.Context) {
	id := c.Param("id")
	email := c.GetString(middleware.EmailKey)
	err := h.uc.Delete(c.Request.Context(), email, id)
	switch {
	case errors.Is(err, goaldomain.ErrGoalNotFound):
		response.Err(c, http.StatusNotFound, "goal not found")
	case err != nil:
		zap.L().Error("failed to delete goal", logger.RequestID(c.Request.Context()), zap.String("id", id), zap.Error(err))
		response.Err(c, http.StatusInternalServerError, "failed to delete goal")
	default:
		c.Status(http.StatusNoContent)
	}
}
