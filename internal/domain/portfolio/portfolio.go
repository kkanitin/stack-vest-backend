package portfolio

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("position not found")
	ErrAlreadyExists = errors.New("position already exists")
)

type Position struct {
	ID        string    `json:"id"`
	UserID    string    `json:"-"`
	Symbol    string    `json:"symbol"`
	Name      string    `json:"name"`
	Shares    float64   `json:"shares"`
	AvgCost   float64   `json:"avgCost"`
	AddedAt   time.Time `json:"addedAt"`
	ValueUsd  float64   `json:"valueUsd"`
	Change24h float64   `json:"change24h"`
}

type Activity struct {
	ID        string    `json:"id"`
	Symbol    string    `json:"symbol,omitempty"`
	Label     string    `json:"label"`
	Detail    string    `json:"detail"`
	Tone      string    `json:"tone"`
	Badge     string    `json:"badge"`
	Timestamp time.Time `json:"timestamp"`
}

type Summary struct {
	TotalValue   float64 `json:"totalValue"`
	Change30d    float64 `json:"change30d"`
	ChangePct30d float64 `json:"changePct30d"`
}

type Repository interface {
	Add(ctx context.Context, userID, symbol, name string, shares, avgCost float64) (*Position, error)
	Remove(ctx context.Context, userID, symbol string) error
	Update(ctx context.Context, userID, symbol string, shares, avgCost *float64) (*Position, error)
	ListByUserID(ctx context.Context, userID string) ([]*Position, error)
	GetActivity(ctx context.Context, userID string, limit int) ([]*Activity, error)
}
