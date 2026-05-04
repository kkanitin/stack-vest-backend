package main

import (
	"github.com/kanitin/stackvest/backend/internal/delivery/http/handler"
	"github.com/kanitin/stackvest/backend/internal/delivery/http/router"
	"github.com/kanitin/stackvest/backend/internal/infrastructure/alphavantage"
	stockuc "github.com/kanitin/stackvest/backend/internal/usecase/stock"
	"github.com/kanitin/stackvest/backend/pkg/config"
)

func main() {
	cfg := config.Load()

	avClient := alphavantage.NewClient(cfg.ThirdPartyAPI.AlphaVantage.APIKey)
	searchUC := stockuc.NewSearchUseCase(avClient)
	stockHandler := handler.NewStockHandler(searchUC)

	r := router.New(stockHandler)
	r.Run(":" + cfg.Server.Port)
}
