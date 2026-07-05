package router

import (
	"log/slog"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/handler"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/middleware"
)

func New(stockHandler *handler.StockHandler, authHandler *handler.AuthHandler, userHandler *handler.UserHandler, watchlistHandler *handler.WatchlistHandler, dcaHandler *handler.DCAHandler, portfolioHandler *handler.PortfolioHandler, popularHandler *handler.PopularHandler, sentimentHandler *handler.SentimentHandler, dividendHandler *handler.DividendHandler, googleClientID string, log *slog.Logger, allowOrigins []string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     allowOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	r.Use(middleware.Logger(log))
	r.Use(middleware.RateLimit(2, 20)) // IP-keyed, applies to all routes incl. public

	r.GET("/health", handler.NewHealthHandler().HealthCheck)

	v1 := r.Group("/api/v1")
	authHandler.RegisterRoutes(v1)
	popularHandler.RegisterRoutes(v1)

	// RateLimit here runs after Auth so it can key per-user (UserIDKey is only set
	// once Auth succeeds); this is in addition to, not instead of, the IP-keyed
	// limiter above.
	protected := v1.Group("", middleware.Auth(googleClientID), middleware.RateLimit(1, 20))
	stockHandler.RegisterRoutes(protected)
	userHandler.RegisterRoutes(protected)
	watchlistHandler.RegisterRoutes(protected)
	dcaHandler.RegisterRoutes(protected)
	portfolioHandler.RegisterRoutes(protected)
	sentimentHandler.RegisterRoutes(protected)
	dividendHandler.RegisterRoutes(protected)

	return r
}
