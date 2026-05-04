package main

import (
	"github.com/kanitin/stackvest/backend/internal/delivery/http/router"
	"github.com/kanitin/stackvest/backend/pkg/config"
)

func main() {
	cfg := config.Load()
	r := router.New()
	r.Run(cfg.ServerAddress)
}
