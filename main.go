package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/kanitin/stackvest/backend/internal/delivery/http/handler"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/router"
	marketdomain "github.com/kanitin/stackvest/backend/internal/domain/market"
	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	"github.com/kanitin/stackvest/backend/internal/infrastructure/cached"
	"github.com/kanitin/stackvest/backend/internal/infrastructure/constituents"
	fmp "github.com/kanitin/stackvest/backend/internal/infrastructure/fmp"
	groq "github.com/kanitin/stackvest/backend/internal/infrastructure/groq"
	"github.com/kanitin/stackvest/backend/internal/infrastructure/throttled"
	dividendrepo "github.com/kanitin/stackvest/backend/internal/repository/dividend"
	marketrepo "github.com/kanitin/stackvest/backend/internal/repository/market"
	portfoliorepo "github.com/kanitin/stackvest/backend/internal/repository/portfolio"
	userrepo "github.com/kanitin/stackvest/backend/internal/repository/user"
	watchlistrepo "github.com/kanitin/stackvest/backend/internal/repository/watchlist"
	analysisuc "github.com/kanitin/stackvest/backend/internal/usecase/analysis"
	authuc "github.com/kanitin/stackvest/backend/internal/usecase/auth"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
	dividenduc "github.com/kanitin/stackvest/backend/internal/usecase/dividend"
	marketuc "github.com/kanitin/stackvest/backend/internal/usecase/market"
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
	"github.com/kanitin/stackvest/backend/pkg/worker"
)

func main() {
	cfg := config.Load()

	log, err := logger.New(cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	zap.ReplaceGlobals(log)
	defer func() { _ = log.Sync() }() // stdout sink is unbuffered; nothing meaningful to flush or fail here

	zap.L().Info("starting StackVest backend", zap.String("port", cfg.Server.Port))

	if cfg.Auth.JWT.Secret == "" {
		zap.L().Error("auth.jwt.secret must be set (config.yaml auth.jwt.secret or AUTH_JWT_SECRET env var)")
		os.Exit(1)
	}

	pool, err := database.NewPostgresPool(context.Background(), cfg.DB.Postgres.DSN)
	if err != nil {
		zap.L().Error("failed to connect to PostgreSQL", zap.Error(err))
		os.Exit(1)
	}

	if cfg.DB.Migrate.Enabled {
		zap.L().Info("running database migrations")
		if err := migrate.Run(cfg.DB.Postgres.DSN); err != nil {
			zap.L().Error("failed to run database migrations", zap.Error(err))
			os.Exit(1)
		}
		zap.L().Info("database migrations complete")
	}

	// Redis backs the dividend calendar cache only. A cold Redis is non-fatal: the
	// dividend endpoint falls back to fetching from FMP directly (logged per request)
	// and caching resumes automatically once Redis is reachable. Every other endpoint
	// is unaffected, so we start the server regardless.
	redisClient, err := cache.NewRedisClient(context.Background(), cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		zap.L().Warn("Redis unavailable at startup; dividend calendar will bypass cache until it recovers", zap.Error(err))
	}

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

	// Adjusted closes for the simulator: reused for 6 hours per symbol and range, so tweaking
	// the amount or frequency does not hit the provider again (see docs/features/dca.md).
	dcaSimulatorUC := dcauc.NewSimulatorUseCase(cached.NewDCAPrices(avClient, 6*time.Hour))
	dcaHandler := handler.NewDCAHandler(dcaSimulatorUC)

	groqClient := groq.NewClient(cfg.ThirdPartyAPI.Groq.APIKey)
	analyzeUC := analysisuc.New(groqClient)

	portfolioRepo := portfoliorepo.NewPostgresRepository(pool)
	// Benchmark index closes: one cached 5-year fetch per symbol serves every range (see
	// docs/features/portfolio.md). Closes only move once a day, so 6 hours is plenty fresh.
	cachedHistory := cached.NewHistoryCloser(avClient, 6*time.Hour)
	portfolioUC := portfoliouc.New(portfolioRepo, userRepo, cachedQuoter, cachedPriceChanger, cfg.Portfolio.MaxPerUser, cfg.Portfolio.MaxPositionsPerPortfolio).
		WithBenchmarks(cachedHistory, portfoliodomain.ParseBenchmarks(cfg.Portfolio.Benchmarks))
	portfolioHandler := handler.NewPortfolioHandler(portfolioUC, analyzeUC)

	// Records every user's total holdings value for the current UTC day: once at
	// startup, then every three hours. Each run replaces the day's row, so a day settles
	// on its last recorded value (see docs/features/portfolio.md). Three hours keeps the
	// FMP cost low (one quote per distinct held symbol per run, whether or not anyone
	// opens the app) while still guaranteeing a run after the US close: the last run of
	// a UTC day falls after 21:00 UTC, and US markets close at 20:00 or 21:00 UTC.
	const valueSnapshotInterval = 3 * time.Hour
	valueSnapshotJob := worker.StartPeriodic(valueSnapshotInterval, func(ctx context.Context) {
		written, skipped, err := portfolioUC.SnapshotValues(ctx, time.Now().UTC())
		if errors.Is(err, context.Canceled) {
			return // shutting down mid-run; nothing was written
		}
		if err != nil {
			zap.L().Error("value snapshot failed", zap.Error(err))
			return
		}
		zap.L().Info("value snapshot recorded", zap.Int("usersWritten", written), zap.Int("usersSkipped", skipped))
	})

	popularHandler := handler.NewPopularHandler(avClient)

	sentimentUC := sentimentuc.NewUseCase(avClient, 6*time.Hour)
	sentimentHandler := handler.NewSentimentHandler(sentimentUC)

	dividendCache := dividendrepo.NewRedisCache(redisClient, 24*time.Hour, time.Hour)
	dividendUC := dividenduc.NewCalendarUseCase(userRepo, portfolioRepo, avClient, dividendCache).WithLedger(portfolioRepo)
	dividendHandler := handler.NewDividendHandler(dividendUC)

	// Index heatmaps: rebuilt in the background and served from memory (see
	// docs/features/market-heatmap.md). Each tile needs a company profile (name, sector,
	// market cap; cached a day) and a price change (all four periods; cached
	// change_ttl_minutes). Cache misses for both share one rate limiter, so a rebuild of
	// ~500 symbols stays inside the FMP per-minute budget that interactive requests share.
	hmCfg := cfg.Market.Heatmap
	// A zero interval would panic the ticker and a zero rate would block forever.
	hmCfg.RefreshMinutes = max(hmCfg.RefreshMinutes, 1)
	hmCfg.CallsPerMinute = max(hmCfg.CallsPerMinute, 1)
	heatmapLimiter := throttled.NewLimiter(hmCfg.CallsPerMinute)
	heatmapChanger := cached.NewPriceChanger(
		throttled.NewPriceChanger(avClient, heatmapLimiter),
		time.Duration(hmCfg.ChangeTTLMinutes)*time.Minute,
	)
	heatmapProfiles := cached.NewProfileFetcher(throttled.NewProfileFetcher(avClient, heatmapLimiter), 24*time.Hour)
	// Index members come from FMP when the plan includes its constituents endpoints, and
	// from the lists bundled in internal/infrastructure/constituents otherwise (Starter).
	heatmapLister := cached.NewConstituentLister(constituents.NewFallbackLister(avClient, constituents.StaticLister{},
		func(index marketdomain.Index, cause error) {
			zap.L().Info("FMP index lists not on plan; using bundled lists", zap.String("index", string(index)), zap.NamedError("cause", cause))
		}), 24*time.Hour)
	// Built snapshots are also kept in Redis for 72h (long enough to span a weekend), so a
	// restart serves the last map at once instead of 503 until the S&P 500 rebuild ends.
	heatmapUC := marketuc.NewHeatmapUseCase(heatmapLister, heatmapProfiles, heatmapChanger).
		WithStore(marketrepo.NewRedisSnapshotStore(redisClient, 72*time.Hour)).
		WithEvents(logHeatmapEvent(hmCfg.CallsPerMinute))
	restoreCtx, cancelRestore := context.WithTimeout(context.Background(), 3*time.Second)
	if n, err := heatmapUC.Restore(restoreCtx); err != nil {
		zap.L().Warn("heatmap snapshots not restored; maps rebuild from scratch", zap.Int("restored", n), zap.Error(err))
	} else {
		zap.L().Info("heatmap snapshots restored", zap.Int("restored", n))
	}
	cancelRestore()
	heatmapJob := worker.StartPeriodic(time.Duration(hmCfg.RefreshMinutes)*time.Minute, func(ctx context.Context) {
		err := heatmapUC.Refresh(ctx)
		if errors.Is(err, context.Canceled) {
			return // shutting down mid-run
		}
		// Each failure was already logged by logHeatmapEvent; this is the run summary.
		if err != nil {
			zap.L().Warn("heatmap refresh finished with failures", zap.Error(err))
			return
		}
		zap.L().Info("heatmap refresh finished")
	})
	marketHandler := handler.NewMarketHandler(heatmapUC)

	healthHandler := handler.NewHealthHandler(pool)

	r := router.New(stockHandler, authHandler, userHandler, watchlistHandler, dcaHandler, portfolioHandler, popularHandler, sentimentHandler, dividendHandler, marketHandler, healthHandler, cfg.Auth.Google.ClientID, log, cfg.CORS.AllowOrigins)

	srv := &http.Server{
		Addr:              ":" + cfg.Server.Port,
		Handler:           r,
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutSeconds) * time.Second,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.Server.IdleTimeoutSeconds) * time.Second,
		// WriteTimeout is intentionally unset: a global deadline would cut off
		// the SSE analysis stream (POST /api/v1/portfolios/:id/analyze).
	}

	runUntilShutdown(srv,
		// Before pool.Close(): the job writes through the pool, so it must have
		// stopped (or been abandoned at the deadline) before the pool goes away.
		valueSnapshotJob.Stop,
		heatmapJob.Stop,
		func(_ context.Context) {
			pool.Close()
		},
		func(_ context.Context) {
			if err := redisClient.Close(); err != nil {
				zap.L().Error("failed to close Redis client", zap.Error(err))
			}
		},
	)
}

// logHeatmapEvent logs index heatmap builds so the logs tell "still building"
// (Info: started, progress, built) apart from "failing" (Error: build failed,
// Warn: built with gaps or not persisted). callsPerMinute is the price-change
// rate limit, used for a rough time-remaining estimate.
func logHeatmapEvent(callsPerMinute int) func(marketuc.Event) {
	return func(e marketuc.Event) {
		idx := zap.String("index", string(e.Index))
		switch e.Kind {
		case marketuc.EventStarted:
			zap.L().Info("heatmap build started", idx, zap.Int("symbols", e.Symbols))
		case marketuc.EventProgress:
			// Two calls per symbol (profile + price change). An upper bound: cached
			// lookups don't wait on the rate limit.
			remainingCalls := 2 * (e.Symbols - e.Done)
			eta := time.Duration(float64(remainingCalls) / float64(callsPerMinute) * float64(time.Minute)).Round(time.Second)
			zap.L().Info("heatmap build in progress, waiting on rate-limited FMP calls", idx,
				zap.Int("done", e.Done), zap.Int("total", e.Symbols), zap.Duration("elapsed", e.Elapsed.Round(time.Second)),
				zap.Duration("etaUpTo", eta))
		case marketuc.EventBuilt:
			fields := []zap.Field{idx, zap.Int("stocks", e.Stocks), zap.Int("symbols", e.Symbols), zap.Duration("elapsed", e.Elapsed.Round(time.Second))}
			if e.MissingProfiles > 0 || e.MissingChanges > 0 {
				// missingProfiles: left off the map (no size); missingChanges: drawn grey.
				zap.L().Warn("heatmap built with gaps: some stocks missing or without changes",
					append(fields, zap.Int("missingProfiles", e.MissingProfiles), zap.Int("missingChanges", e.MissingChanges),
						zap.NamedError("sampleError", e.SampleErr))...)
				return
			}
			zap.L().Info("heatmap built", fields...)
		case marketuc.EventFailed:
			zap.L().Error("heatmap build failed", idx, zap.Error(e.Err),
				zap.Duration("elapsed", e.Elapsed.Round(time.Second)), zap.Bool("servingPrevious", e.ServingPrevious))
		case marketuc.EventSaveFailed:
			zap.L().Warn("heatmap snapshot not saved to Redis; a restart will rebuild it", idx, zap.Error(e.Err))
		}
	}
}

func runUntilShutdown(srv *http.Server, cleanups ...func(context.Context)) {
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zap.L().Error("server error", zap.Error(err))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	zap.L().Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		zap.L().Error("server forced to shutdown", zap.Error(err))
	}

	for _, fn := range cleanups {
		fn(ctx)
	}

	zap.L().Info("server stopped")
}
