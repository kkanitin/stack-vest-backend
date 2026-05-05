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
	"github.com/kanitin/stackvest/backend/internal/infrastructure/alphavantage"
	userrepo "github.com/kanitin/stackvest/backend/internal/repository/user"
	authuc "github.com/kanitin/stackvest/backend/internal/usecase/auth"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
	"github.com/kanitin/stackvest/backend/pkg/config"
	"github.com/kanitin/stackvest/backend/pkg/database"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

func main() {
	cfg := config.Load()

	log := logger.New(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	slog.Info("starting StackVest backend", "port", cfg.Server.Port)

	mongoClient, err := database.NewMongoClient(cfg.DB.Mongo.URI)
	if err != nil {
		slog.Error("failed to connect to MongoDB", "error", err)
		os.Exit(1)
	}

	dbName := cfg.DB.Mongo.Name
	if dbName == "" {
		dbName = "stackvest"
	}
	db := database.NewDatabase(mongoClient, dbName)

	avClient := alphavantage.NewClient(cfg.ThirdPartyAPI.AlphaVantage.APIKey)
	searchUC := stockuc.NewSearchUseCase(avClient)
	stockHandler := handler.NewStockHandler(searchUC)

	userRepo := userrepo.NewMongoRepository(db)
	googleUC := authuc.NewGoogleUseCase(
		cfg.Auth.Google.ClientID,
		cfg.Auth.Google.ClientSecret,
		cfg.Auth.Google.RedirectURL,
		userRepo,
	)
	authHandler := handler.NewAuthHandler(googleUC, cfg.Auth.JWT.Secret)

	r := router.New(stockHandler, authHandler, log)

	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: r,
	}

	runUntilShutdown(srv,
		func(ctx context.Context) {
			if err := mongoClient.Disconnect(ctx); err != nil {
				slog.Error("failed to disconnect MongoDB", "error", err)
			}
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
