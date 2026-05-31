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

	"github.com/kanitin/stackvest/backend/internal/delivery/http/handler"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/router"
	fmp "github.com/kanitin/stackvest/backend/internal/infrastructure/fmp"
	portfoliorepo "github.com/kanitin/stackvest/backend/internal/repository/portfolio"
	userrepo "github.com/kanitin/stackvest/backend/internal/repository/user"
	watchlistrepo "github.com/kanitin/stackvest/backend/internal/repository/watchlist"
	authuc "github.com/kanitin/stackvest/backend/internal/usecase/auth"
	dcauc "github.com/kanitin/stackvest/backend/internal/usecase/dca"
	portfoliouc "github.com/kanitin/stackvest/backend/internal/usecase/portfolio"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
	useruc "github.com/kanitin/stackvest/backend/internal/usecase/user"
	watchlistuc "github.com/kanitin/stackvest/backend/internal/usecase/watchlist"
	"github.com/kanitin/stackvest/backend/pkg/config"
	"github.com/kanitin/stackvest/backend/pkg/database"
	"github.com/kanitin/stackvest/backend/pkg/logger"
	"github.com/kanitin/stackvest/backend/pkg/migrate"
)

func main() {
	cfg := config.Load()

	log := logger.New(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	slog.Info("starting StackVest backend", "port", cfg.Server.Port)

	pool, err := database.NewPostgresPool(context.Background(), cfg.DB.Postgres.DSN)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", "error", err)
		os.Exit(1)
	}

	if cfg.DB.Migrate.Enabled {
		slog.Info("running database migrations")
		if err := migrate.Run(cfg.DB.Postgres.DSN); err != nil {
			slog.Error("failed to run database migrations", "error", err)
			os.Exit(1)
		}
		slog.Info("database migrations complete")
	}

	userRepo := userrepo.NewPostgresRepository(pool)

	avClient := fmp.NewClient(cfg.ThirdPartyAPI.FMP.APIKey)
	searchUC := stockuc.NewSearchUseCase(avClient)
	priceChangeUC := stockuc.NewPriceChangeUseCase(avClient)
	quoteUC := stockuc.NewQuoteUseCase(avClient)
	historyUC := stockuc.NewHistoryUseCase(avClient)
	stockHandler := handler.NewStockHandler(searchUC, priceChangeUC, quoteUC, historyUC)

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

	portfolioRepo := portfoliorepo.NewPostgresRepository(pool)
	portfolioUC := portfoliouc.New(portfolioRepo, userRepo, avClient, avClient)
	portfolioHandler := handler.NewPortfolioHandler(portfolioUC)

	popularHandler := handler.NewPopularHandler(avClient)

	r := router.New(stockHandler, authHandler, userHandler, watchlistHandler, dcaHandler, portfolioHandler, popularHandler, cfg.Auth.Google.ClientID, log, cfg.CORS.AllowOrigins)

	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: r,
	}

	runUntilShutdown(srv,
		func(_ context.Context) {
			pool.Close()
		},
	)
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
