package router

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/handler"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
)

func New(stockHandler *handler.StockHandler, authHandler *handler.AuthHandler, log *slog.Logger) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger(log))

	r.GET("/health", handler.NewHealthHandler().HealthCheck)

	v1 := r.Group("/api/v1")
	stockHandler.RegisterRoutes(v1)
	authHandler.RegisterRoutes(v1)

	return r
}
