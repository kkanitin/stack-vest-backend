package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/response"
)

type HealthHandler struct {
	pool *pgxpool.Pool
}

func NewHealthHandler(pool *pgxpool.Pool) *HealthHandler {
	return &HealthHandler{pool: pool}
}

// RegisterRoutes mounts the detailed, authenticated health routes. The public
// /health probe is mounted directly on the engine in router.go.
func (h *HealthHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/health/db", h.dbPoolStats)
}

// HealthCheck is the /health exception to the standard response envelope (see
// AGENTS.md). It is public (uptime monitors use it to keep the instance warm),
// so it deliberately reveals nothing beyond liveness.
func (h *HealthHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "ready"})
}

type dbPoolStatsResponse struct {
	MaxConns          int32 `json:"maxConns"`
	TotalConns        int32 `json:"totalConns"`
	IdleConns         int32 `json:"idleConns"`
	AcquireCount      int64 `json:"acquireCount"`
	EmptyAcquireCount int64 `json:"emptyAcquireCount"`
	AcquireDurationMs int64 `json:"acquireDurationMs"`
}

// dbPoolStats exposes pgxpool.Pool.Stat() so pool sizing (currently at library
// defaults) can be measured against real traffic before it's tuned.
func (h *HealthHandler) dbPoolStats(c *gin.Context) {
	stat := h.pool.Stat()
	response.OK(c, dbPoolStatsResponse{
		MaxConns:          stat.MaxConns(),
		TotalConns:        stat.TotalConns(),
		IdleConns:         stat.IdleConns(),
		AcquireCount:      stat.AcquireCount(),
		EmptyAcquireCount: stat.EmptyAcquireCount(),
		AcquireDurationMs: stat.AcquireDuration().Milliseconds(),
	})
}
