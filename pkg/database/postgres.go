package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool builds a connection pool with explicit sizing.
//
// minConns matters for latency, not for startup: pgxpool opens those idle
// connections on a background goroutine and returns immediately, so pre-warming
// costs nothing at boot while sparing the first request a cold connect. Leaving
// minConns at pgx's default of 0 means the pool never warms at all, and every
// request that arrives after an idle window pays the full handshake.
//
// Note this does not verify the database is reachable — only a malformed DSN
// fails here. Connection errors surface on first use.
func NewPostgresPool(ctx context.Context, dsn string, minConns, maxConns int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}
	if minConns > 0 {
		// A minimum above the maximum would leave the pool permanently trying to
		// open connections it is not allowed to have.
		if int32(minConns) > cfg.MaxConns {
			minConns = int(cfg.MaxConns)
		}
		cfg.MinConns = int32(minConns)
		cfg.MinIdleConns = int32(minConns)
	}

	return pgxpool.NewWithConfig(ctx, cfg)
}
