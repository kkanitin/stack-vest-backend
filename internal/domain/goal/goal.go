package goal

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrGoalNotFound is returned for a goal that does not exist, has a malformed id,
	// or belongs to another user (404, never 403).
	ErrGoalNotFound = errors.New("goal not found")
	// ErrInvalidPortfolio is returned when portfolioId does not name one of the user's portfolios.
	ErrInvalidPortfolio = errors.New("portfolio not found")
	// ErrGoalLimitReached is returned when the user already owns the maximum number of goals.
	ErrGoalLimitReached = errors.New("goal limit reached")
)

// Kind says what a goal simulates.
type Kind string

const (
	// KindProjection projects a fixed monthly amount forward; monthlyAmount is required.
	KindProjection Kind = "projection"
	// KindTarget works back from a target amount; targetAmount is required.
	KindTarget Kind = "target"
)

// DefaultCurrency is stored when a goal is saved without a currency.
const DefaultCurrency = "USD"

// Goal holds the inputs of a saved financial goal. Results are calculated by the client.
// Percentages (expectedReturn, contributionGrowth, inflation) are annual, e.g. 7 means 7%.
type Goal struct {
	ID                 string    `json:"id"`
	UserID             string    `json:"-"`
	PortfolioID        *string   `json:"portfolioId"`
	Name               string    `json:"name"`
	Kind               Kind      `json:"kind"`
	Currency           string    `json:"currency"`
	StartingAmount     float64   `json:"startingAmount"`
	MonthlyAmount      *float64  `json:"monthlyAmount"`
	TargetAmount       *float64  `json:"targetAmount"`
	Years              int       `json:"years"`
	ExpectedReturn     float64   `json:"expectedReturn"`
	ContributionGrowth *float64  `json:"contributionGrowth"`
	Inflation          *float64  `json:"inflation"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type Repository interface {
	// Create inserts g for g.UserID, refusing with ErrGoalLimitReached when the user
	// already has maxPerUser goals (checked atomically with the insert).
	Create(ctx context.Context, g *Goal, maxPerUser int) (*Goal, error)
	// ListByUser returns one page of the user's goals, newest first, and the total count.
	ListByUser(ctx context.Context, userID string, limit, offset int) ([]*Goal, int, error)
	// Get returns ErrGoalNotFound for a missing or malformed id.
	Get(ctx context.Context, id string) (*Goal, error)
	// Update replaces every input field of the goal g.ID owned by g.UserID.
	Update(ctx context.Context, g *Goal) (*Goal, error)
	Delete(ctx context.Context, id string) error
}
