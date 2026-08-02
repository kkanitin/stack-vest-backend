package cache

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedisClient builds a Redis client and verifies the connection with a PING
// in the background. The caller owns the returned client and is responsible for
// closing it on shutdown.
//
// The ping is deliberately asynchronous: go-redis dials lazily and reconnects on
// its own, so a cold Redis is not a reason to delay startup. Verifying it inline
// would stall boot for a full round trip when Redis is healthy and up to 5s when
// it is not, for no benefit beyond the warning this logs anyway.
//
// The returned client is always usable, so callers that tolerate a cold Redis can
// wire it up unconditionally and caching resumes once Redis is reachable.
func NewRedisClient(ctx context.Context, addr, password string, db int) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// Bounded and self-terminating, so it needs no shutdown cleanup of its own;
	// the caller's Close covers the client.
	go func() {
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := client.Ping(pingCtx).Err(); err != nil {
			slog.Warn("Redis unreachable; dividend calendar will bypass cache until it recovers", "addr", addr, "error", err)
		}
	}()

	return client
}
