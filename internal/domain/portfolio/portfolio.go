package portfolio

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("position not found")
	ErrAlreadyExists = errors.New("position already exists")

	ErrPortfolioNotFound     = errors.New("portfolio not found")
	ErrPortfolioLimitReached = errors.New("portfolio limit reached")
	ErrPositionLimitReached  = errors.New("position limit reached")
)

type Portfolio struct {
	ID          string    `json:"id"`
	UserID      string    `json:"-"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Position struct {
	ID          string    `json:"id"`
	PortfolioID string    `json:"-"`
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	Shares      float64   `json:"shares"`
	AvgCost     float64   `json:"avgCost"`
	AddedAt     time.Time `json:"addedAt"`
	ValueUsd    float64   `json:"valueUsd"`
	Change24h   float64   `json:"change24h"`
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
	// Portfolios
	CreatePortfolio(ctx context.Context, userID, name, description string) (*Portfolio, error)
	ListPortfolios(ctx context.Context, userID string) ([]*Portfolio, error)
	GetPortfolio(ctx context.Context, id string) (*Portfolio, error)
	UpdatePortfolio(ctx context.Context, id string, name, description *string) (*Portfolio, error)
	DeletePortfolio(ctx context.Context, id string) error
	CountPortfolios(ctx context.Context, userID string) (int, error)

	// Positions (scoped to a portfolio)
	Add(ctx context.Context, portfolioID, symbol, name string, shares, avgCost float64) (*Position, error)
	Remove(ctx context.Context, portfolioID, symbol string) error
	Update(ctx context.Context, portfolioID, symbol string, shares, avgCost *float64) (*Position, error)
	ListByPortfolioID(ctx context.Context, portfolioID string) ([]*Position, error)
	CountPositions(ctx context.Context, portfolioID string) (int, error)
	GetActivity(ctx context.Context, portfolioID string, limit int) ([]*Activity, error)
}
