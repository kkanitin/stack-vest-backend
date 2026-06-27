package dividend

import (
	"context"
	"time"
)

// Event is a single dividend record for a symbol, as published by the upstream
// market-data provider. It is market-wide reference data: the dates and amount are
// identical for every holder of the symbol, so a record is fetched once and shared
// across all users. Date fields are zero (time.IsZero) when the provider omits them.
type Event struct {
	Symbol          string    `json:"symbol"`
	ExDate          time.Time `json:"exDate"`
	RecordDate      time.Time `json:"recordDate"`
	PaymentDate     time.Time `json:"paymentDate"`
	DeclarationDate time.Time `json:"declarationDate"`
	Dividend        float64   `json:"dividend"`    // per share
	AdjDividend     float64   `json:"adjDividend"` // split-adjusted, per share
	Yield           float64   `json:"yield"`
	Frequency       string    `json:"frequency"`
}

// CalendarEntry is an Event projected onto a specific user's holding: the shares
// they hold of the symbol and the resulting estimated payout for that event.
type CalendarEntry struct {
	Event
	Shares          float64 `json:"shares"`
	EstimatedAmount float64 `json:"estimatedAmount"` // Shares * Dividend
}

// Fetcher retrieves the market-wide dividend calendar for a date range from the
// upstream provider (every symbol's upcoming dividends in [from, to]). The
// per-symbol endpoint returns only history, so the calendar endpoint is the source
// for forward-looking payouts. Implemented by *fmp.Client.
type Fetcher interface {
	GetDividendsCalendar(from, to time.Time) ([]Event, error)
}

// Cache is a shared, key-addressed cache of dividend events (Redis-backed in prod).
// Because the calendar is market-wide reference data, a single cached blob (keyed by
// the fetch window) serves every user. Get's second return value reports whether the
// key was present — an empty, non-nil slice with found=true is a valid hit (negative
// caching), distinct from a miss (found=false).
type Cache interface {
	Get(ctx context.Context, key string) ([]Event, bool, error)
	Set(ctx context.Context, key string, events []Event) error
}
