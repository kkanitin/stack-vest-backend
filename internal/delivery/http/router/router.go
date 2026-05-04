package router

import (
	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/handler"
)

func New() *gin.Engine {
	r := gin.Default()

	health := handler.NewHealthHandler()
	r.GET("/ping", health.Ping)

	return r
}
