// Command migrate applies pending database migrations and exits.
//
// It exists so the API server does not have to. Running migrations in-process
// costs every boot a separate connection handshake plus a chain of sequential
// round trips, even when the schema is already up to date — a real delay against
// a remote database. As a deploy step it runs once, before the server starts.
//
// Usage: build alongside the server and run it ahead of rollout.
//
//	go run ./cmd/migrate
//
// Reads the same config.yaml and environment overrides as the server, so it needs
// no separate configuration. Exits non-zero if migrations fail, so a deploy
// pipeline can halt before starting the new server.
package main

import (
	"log/slog"
	"os"

	"github.com/kanitin/stackvest/backend/pkg/config"
	"github.com/kanitin/stackvest/backend/pkg/logger"
	"github.com/kanitin/stackvest/backend/pkg/migrate"
)

func main() {
	cfg := config.Load()

	slog.SetDefault(logger.New(cfg.Log.Level, cfg.Log.Format))

	slog.Info("running database migrations")
	if err := migrate.Run(cfg.DB.Postgres.DSN); err != nil {
		slog.Error("failed to run database migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("database migrations complete")
}
