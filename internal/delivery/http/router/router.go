package router

import (
	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/handler"
)

func New(stockHandler *handler.StockHandler) *gin.Engine {
	r := gin.Default()

	r.GET("/ping", handler.NewHealthHandler().Ping)

	v1 := r.Group("/api/v1")
	stockHandler.RegisterRoutes(v1)

	return r
}
