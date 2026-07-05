package watchlist

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("watchlist item not found")
	ErrAlreadyExists = errors.New("watchlist item already exists")
	ErrInvalidSymbol = errors.New("invalid symbol")
)

type Item struct {
	ID            string    `json:"id"`
	UserID        string    `json:"userId"`
	Symbol        string    `json:"symbol"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	AddedAt       time.Time `json:"addedAt"`
	AlertsEnabled bool      `json:"alertsEnabled"`
	Category      []string  `json:"category"`
}

type Repository interface {
	Add(ctx context.Context, item *Item) (*Item, error)
	Remove(ctx context.Context, userID, symbol string) error
	ListByUserID(ctx context.Context, userID string) ([]Item, error)
	SetAlertsEnabled(ctx context.Context, userID, symbol string, enabled bool) (*Item, error)
}
