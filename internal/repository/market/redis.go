package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	marketdomain "github.com/kanitin/stackvest/backend/internal/domain/market"
)

// keyPrefix is versioned so the stored encoding can change without colliding
// with snapshots written by a previous shape.
const keyPrefix = "heatmap:v1:"

// RedisSnapshotStore keeps the latest heatmap per index in Redis, so a restarted
// server can serve it straight away. It implements marketdomain.SnapshotStore.
type RedisSnapshotStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisSnapshotStore builds a store whose snapshots expire after ttl, so a
// server that has been down for long never serves very old prices.
func NewRedisSnapshotStore(client *redis.Client, ttl time.Duration) *RedisSnapshotStore {
	return &RedisSnapshotStore{client: client, ttl: ttl}
}

func (s *RedisSnapshotStore) Load(ctx context.Context, index marketdomain.Index) (*marketdomain.Heatmap, error) {
	key := keyPrefix + string(index)
	data, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get %s: %w", key, err)
	}
	var hm marketdomain.Heatmap
	if err := json.Unmarshal(data, &hm); err != nil {
		return nil, fmt.Errorf("heatmap snapshot decode %s: %w", key, err)
	}
	return &hm, nil
}

func (s *RedisSnapshotStore) Save(ctx context.Context, hm *marketdomain.Heatmap) error {
	key := keyPrefix + string(hm.Index)
	data, err := json.Marshal(hm)
	if err != nil {
		return fmt.Errorf("heatmap snapshot encode %s: %w", key, err)
	}
	if err := s.client.Set(ctx, key, data, s.ttl).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key, err)
	}
	return nil
}

var _ marketdomain.SnapshotStore = (*RedisSnapshotStore)(nil)
