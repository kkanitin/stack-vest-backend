package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedisClient dials Redis and verifies the connection with a PING. The caller
// owns the returned client and is responsible for closing it on shutdown.
//
// The client is always returned non-nil, even when the ping fails: go-redis
// reconnects lazily, so a caller that chooses to tolerate a cold Redis can keep
// using it and caching will resume once Redis is reachable. A non-nil error means
// the initial ping failed; the caller decides whether that is fatal.
func NewRedisClient(ctx context.Context, addr, password string, db int) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		return client, fmt.Errorf("redis ping failed: %w", err)
	}
	return client, nil
}
