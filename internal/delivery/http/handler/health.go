package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthHandler struct {
	pool *pgxpool.Pool
}

func NewHealthHandler(pool *pgxpool.Pool) *HealthHandler {
	return &HealthHandler{pool: pool}
}

// HealthCheck is the /health exception to the standard response envelope (see
// AGENTS.md). dbPool exposes pgxpool.Pool.Stat() so pool sizing (currently at
// library defaults — see docs/performance-review.md #4) can be measured
// against real traffic before it's tuned.
func (h *HealthHandler) HealthCheck(c *gin.Context) {
	stat := h.pool.Stat()
	c.JSON(http.StatusOK, gin.H{
		"message": "ready",
		"dbPool": gin.H{
			"maxConns":          stat.MaxConns(),
			"totalConns":        stat.TotalConns(),
			"idleConns":         stat.IdleConns(),
			"acquireCount":      stat.AcquireCount(),
			"emptyAcquireCount": stat.EmptyAcquireCount(),
			"acquireDurationMs": stat.AcquireDuration().Milliseconds(),
		},
	})
}
