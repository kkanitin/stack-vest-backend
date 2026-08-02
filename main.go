package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/handler"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/router"
	"github.com/kanitin/stackvest/backend/internal/infrastructure/cached"
	fmp "github.com/kanitin/stackvest/backend/internal/infrastructure/fmp"
	groq "github.com/kanitin/stackvest/backend/internal/infrastructure/groq"
	dividendrepo "github.com/kanitin/stackvest/backend/internal/repository/dividend"
	portfoliorepo "github.com/kanitin/stackvest/backend/internal/repository/portfolio"
	userrepo "github.com/kanitin/stackvest/backend/internal/repository/user"
	watchlistrepo "github.com/kanitin/stackvest/backend/internal/repository/watchlist"
	analysisuc "github.com/kanitin/stackvest/backend/internal/usecase/analysis"
	authuc "github.com/kanitin/stackvest/backend/internal/usecase/auth"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
	dividenduc "github.com/kanitin/stackvest/backend/internal/usecase/dividend"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
	sentimentuc "github.com/kanitin/stackvest/backend/internal/usecase/sentiment"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
	useruc "github.com/kanitin/stackvest/backend/internal/usecase/user"
	watchlistuc "github.com/kanitin/stackvest/backend/internal/usecase/watchlist"
	"github.com/kanitin/stackvest/backend/pkg/cache"
	"github.com/kanitin/stackvest/backend/pkg/config"
	"github.com/kanitin/stackvest/backend/pkg/database"
	"github.com/kanitin/stackvest/backend/pkg/logger"
	"github.com/kanitin/stackvest/backend/pkg/migrate"
)

func main() {
	bootStart := time.Now()

	cfg := config.Load()
	configElapsed := time.Since(bootStart)

	log := logger.New(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	slog.Info("starting StackVest backend", "port", cfg.Server.Port)
	logPhase("config", configElapsed)

	if cfg.Auth.JWT.Secret == "" {
		slog.Error("auth.jwt.secret must be set (config.yaml auth.jwt.secret or AUTH_JWT_SECRET env var)")
		os.Exit(1)
	}

	poolStart := time.Now()
	pool, err := database.NewPostgresPool(context.Background(), cfg.DB.Postgres.DSN, cfg.DB.Postgres.MinConns, cfg.DB.Postgres.MaxConns)
	if err != nil {
		// Only a bad DSN or pool config fails here — the pool connects lazily in
		// the background, so an unreachable database surfaces on first use.
		slog.Error("invalid PostgreSQL configuration", "error", err)
		os.Exit(1)
	}
	logPhase("pgpool", time.Since(poolStart))

	// Migrations are normally a deploy step (cmd/migrate) rather than part of boot:
	// in-process they cost a separate connection handshake plus a chain of sequential
	// round trips on every start, even when nothing is pending. The switch stays for
	// local development.
	if cfg.DB.Migrate.Enabled {
		migrateStart := time.Now()
		slog.Info("running database migrations")
		if err := migrate.Run(cfg.DB.Postgres.DSN); err != nil {
			slog.Error("failed to run database migrations", "error", err)
			os.Exit(1)
		}
		slog.Info("database migrations complete")
		logPhase("migrate", time.Since(migrateStart))
	} else {
		slog.Info("skipping database migrations; run cmd/migrate as a deploy step", "reason", "db.migrate.enabled=false")
	}

	wiringStart := time.Now()

	// Redis backs the dividend calendar cache only. A cold Redis is non-fatal: the
	// dividend endpoint falls back to fetching from FMP directly (logged per request)
	// and caching resumes automatically once Redis is reachable. Every other endpoint
	// is unaffected, so we start the server regardless — the connection is verified
	// asynchronously inside NewRedisClient and never delays boot.
	redisClient := cache.NewRedisClient(context.Background(), cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)

	userRepo := userrepo.NewPostgresRepository(pool)

	avClient := fmp.NewClient(cfg.ThirdPartyAPI.FMP.APIKey)

	// Caching decorators for Quoter/PriceChanger, wrapped once here so every
	// consumer below shares one cache instead of each hitting FMP directly.
	// Short TTL: quotes are live market data, so staleness must stay bounded.
	const quoteCacheTTL = 30 * time.Second
	cachedQuoter := cached.NewQuoter(avClient, quoteCacheTTL)
	cachedPriceChanger := cached.NewPriceChanger(avClient, quoteCacheTTL)

	searchUC := stockuc.NewSearchUseCase(avClient, avClient, 24*time.Hour, 5*time.Minute)
	priceChangeUC := stockuc.NewPriceChangeUseCase(cachedPriceChanger)
	quoteUC := stockuc.NewQuoteUseCase(cachedQuoter)
	historyUC := stockuc.NewHistoryUseCase(avClient)
	batchPriceChangeUC := stockuc.NewBatchPriceChangeUseCase(cachedPriceChanger)
	batchHistoryUC := stockuc.NewBatchHistoryUseCase(avClient)
	profileUC := stockuc.NewProfileUseCase(avClient)
	stockHandler := handler.NewStockHandler(searchUC, priceChangeUC, quoteUC, historyUC, batchPriceChangeUC, batchHistoryUC, profileUC)

	googleUC := authuc.NewGoogleUseCase(
		cfg.Auth.Google.ClientID,
		cfg.Auth.Google.ClientSecret,
		cfg.Auth.Google.RedirectURL,
		userRepo,
	)
	authHandler := handler.NewAuthHandler(googleUC, cfg.Auth.JWT.Secret)

	userUC := useruc.NewUserUseCase(userRepo)
	userHandler := handler.NewUserHandler(userUC)

	watchlistRepo := watchlistrepo.NewPostgresRepository(pool)
	watchlistUC := watchlistuc.NewWatchlistUseCase(watchlistRepo, userRepo, avClient)
	watchlistHandler := handler.NewWatchlistHandler(watchlistUC)

	dcaSimulatorUC := dcauc.NewSimulatorUseCase(avClient)
	dcaHandler := handler.NewDCAHandler(dcaSimulatorUC)

	groqClient := groq.NewClient(cfg.ThirdPartyAPI.Groq.APIKey)
	analyzeUC := analysisuc.New(groqClient)

	portfolioRepo := portfoliorepo.NewPostgresRepository(pool)
	portfolioUC := portfoliouc.New(portfolioRepo, userRepo, cachedQuoter, cachedPriceChanger, cfg.Portfolio.MaxPerUser, cfg.Portfolio.MaxPositionsPerPortfolio)
	portfolioHandler := handler.NewPortfolioHandler(portfolioUC, analyzeUC)

	popularHandler := handler.NewPopularHandler(avClient)

	sentimentUC := sentimentuc.NewUseCase(avClient, 6*time.Hour)
	sentimentHandler := handler.NewSentimentHandler(sentimentUC)

	dividendCache := dividendrepo.NewRedisCache(redisClient, 24*time.Hour, time.Hour)
	dividendUC := dividenduc.NewCalendarUseCase(userRepo, portfolioRepo, avClient, dividendCache)
	dividendHandler := handler.NewDividendHandler(dividendUC)

	healthHandler := handler.NewHealthHandler(pool)

	// Set here rather than inside router.New: gin.SetMode is process-global, and the
	// handler and middleware tests select gin.TestMode before building a router.
	gin.SetMode(gin.ReleaseMode)

	r := router.New(stockHandler, authHandler, userHandler, watchlistHandler, dcaHandler, portfolioHandler, popularHandler, sentimentHandler, dividendHandler, healthHandler, cfg.Auth.Google.ClientID, log, cfg.CORS.AllowOrigins)

	logPhase("wiring", time.Since(wiringStart))

	srv := &http.Server{
		Addr:              ":" + cfg.Server.Port,
		Handler:           r,
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutSeconds) * time.Second,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.Server.IdleTimeoutSeconds) * time.Second,
		// WriteTimeout is intentionally unset: a global deadline would cut off
		// the SSE analysis stream (POST /api/v1/portfolios/:id/analyze).
	}

	slog.Info("boot complete", "elapsedMs", time.Since(bootStart).Milliseconds())

	runUntilShutdown(srv,
		func(_ context.Context) {
			pool.Close()
		},
		func(_ context.Context) {
			if err := redisClient.Close(); err != nil {
				slog.Error("failed to close Redis client", "error", err)
			}
		},
	)
}

// logPhase records how long one step of startup took. Everything up to
// ListenAndServe runs sequentially, so these add up to time-to-serving and show
// which step to attack when boot gets slow.
func logPhase(name string, elapsed time.Duration) {
	slog.Info("boot phase", "phase", name, "elapsedMs", elapsed.Milliseconds())
}

func runUntilShutdown(srv *http.Server, cleanups ...func(context.Context)) {
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	for _, fn := range cleanups {
		fn(ctx)
	}

	slog.Info("server stopped")
}
