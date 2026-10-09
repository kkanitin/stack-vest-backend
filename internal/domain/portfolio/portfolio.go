package portfolio

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("position not found")
	ErrAlreadyExists = errors.New("position already exists")

	ErrPortfolioNotFound     = errors.New("portfolio not found")
	ErrPortfolioLimitReached = errors.New("portfolio limit reached")
	// ErrUnknownBenchmark is returned when a history is requested against a benchmark
	// symbol that is not in the configured list.
	ErrUnknownBenchmark     = errors.New("unknown benchmark")
	ErrPositionLimitReached = errors.New("position limit reached")

	// ErrPortfolioEmpty is returned when an analysis is requested for a portfolio that
	// has no holdings. ErrPricingUnavailable is returned when it has holdings but none
	// could be priced, so no weights can be computed.
	ErrPortfolioEmpty     = errors.New("portfolio has no holdings to analyze")
	ErrPricingUnavailable = errors.New("pricing data unavailable")
)

type Portfolio struct {
	ID          string    `json:"id"`
	UserID      string    `json:"-"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// Value and AssetCount are derived (not persisted): the current USD value of the
	// portfolio's holdings and the number of holdings. They are populated only on the
	// List/Get responses; Create/Update leave them nil (→ JSON null) since those paths
	// don't load holdings. Value is also nil when holdings exist but none could be
	// priced (upstream quote outage), so clients can render "—" rather than a false $0.
	Value      *float64 `json:"value"`
	AssetCount *int     `json:"assetCount"`
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
	// Derived from the ledger and live price (RealisedPnl is also cached in the DB).
	// CostBasis = Shares*AvgCost. UnrealisedPnl/Pct are 0 when the price is unavailable
	// or the holding is closed. Closed means Shares == 0 (listed only with includeClosed).
	CostBasis        float64 `json:"costBasis"`
	UnrealisedPnl    float64 `json:"unrealisedPnl"`
	UnrealisedPnlPct float64 `json:"unrealisedPnlPct"`
	RealisedPnl      float64 `json:"realisedPnl"`
	Closed           bool    `json:"closed"`
}

type Activity struct {
	ID        string    `json:"id"`
	Symbol    string    `json:"symbol,omitempty"`
	Label     string    `json:"label"`
	Detail    string    `json:"detail"`
	Tone      string    `json:"tone"`
	Badge     string    `json:"badge"`
	Timestamp time.Time `json:"timestamp"`
	// PortfolioID and PortfolioName say which portfolio the row belongs to. They are set
	// only on the cross-portfolio feed; the per-portfolio feed omits them.
	PortfolioID   string `json:"portfolioId,omitempty"`
	PortfolioName string `json:"portfolioName,omitempty"`
}

type Summary struct {
	TotalValue   float64 `json:"totalValue"`
	Change30d    float64 `json:"change30d"`
	ChangePct30d float64 `json:"changePct30d"`
	// Change30d/ChangePct30d are the 30-day time-weighted return (excludes deposits).
	RealisedPnl   float64 `json:"realisedPnl"`
	UnrealisedPnl float64 `json:"unrealisedPnl"`
}

// PortfoliosSummary aggregates figures across all of a user's portfolios for the
// dashboard header.
type PortfoliosSummary struct {
	TotalValue    float64 `json:"totalValue"`
	ChangePct     float64 `json:"changePct"` // 30-day time-weighted return across all portfolios
	RealisedPnl   float64 `json:"realisedPnl"`
	UnrealisedPnl float64 `json:"unrealisedPnl"`
	// DiversificationScore is 0–100, derived from holding-value concentration
	// (HHI): a single holding scores 0, evenly spread holdings approach 100.
	DiversificationScore int `json:"diversificationScore"`
}

// UserHolding is one position with its owner, as read for the cross-user value snapshot.
type UserHolding struct {
	UserID string
	Symbol string
	Shares float64
}

// ValuePoint is the total USD value of a user's holdings, across all portfolios, as
// last recorded on one UTC calendar day.
type ValuePoint struct {
	Date  string  `json:"date"` // YYYY-MM-DD
	Value float64 `json:"value"`
	// BenchmarkClose is the benchmark index close on Date, or the latest earlier close when
	// the market was closed that day. Nil (omitted) when no close exists or no benchmark
	// was requested.
	BenchmarkClose *float64 `json:"benchmarkClose,omitempty"`
	// ReturnPct is the cumulative time-weighted return (%) from the first point of the
	// range to this point; 0 on the first point.
	ReturnPct float64 `json:"returnPct"`
}

// ValueHistory is the recorded value series for a range, oldest first. Benchmark is set
// only when the caller asked for a benchmark overlay.
type ValueHistory struct {
	Range     HistoryRange   `json:"range"`
	Points    []*ValuePoint  `json:"points"`
	Benchmark *BenchmarkInfo `json:"benchmark,omitempty"`
}

// Benchmark is a market index (or ETF proxy) a portfolio's value can be compared with.
type Benchmark struct {
	Symbol string `json:"symbol"`
	Label  string `json:"label"`
}

// BenchmarkInfo describes the benchmark attached to a ValueHistory. Available is false
// when the index data could not be fetched, so clients can hide the overlay.
type BenchmarkInfo struct {
	Symbol    string `json:"symbol"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
}

// ParseBenchmarks turns config entries of the form "SYMBOL=Label" into Benchmarks.
// Entries are trimmed, the symbol is upper-cased, and the label falls back to the symbol
// when omitted. Entries with an empty symbol are skipped.
func ParseBenchmarks(entries []string) []Benchmark {
	out := make([]Benchmark, 0, len(entries))
	for _, e := range entries {
		sym, label, _ := strings.Cut(e, "=")
		sym = strings.ToUpper(strings.TrimSpace(sym))
		label = strings.TrimSpace(label)
		if sym == "" {
			continue
		}
		if label == "" {
			label = sym
		}
		out = append(out, Benchmark{Symbol: sym, Label: label})
	}
	return out
}

// HistoryRange selects how far back a value history reaches. The values match the
// batch stock-history ranges so the frontend uses one vocabulary.
type HistoryRange string

const (
	HistoryRange7D  HistoryRange = "7D"
	HistoryRange30D HistoryRange = "30D"
	HistoryRange90D HistoryRange = "90D"
	HistoryRange1Y  HistoryRange = "1Y"
	HistoryRangeAll HistoryRange = "All"
)

// Days is the look-back in days; bounded is false for HistoryRangeAll, which has no cutoff.
func (r HistoryRange) Days() (days int, bounded bool) {
	switch r {
	case HistoryRange7D:
		return 7, true
	case HistoryRange30D:
		return 30, true
	case HistoryRange90D:
		return 90, true
	case HistoryRange1Y:
		return 365, true
	}
	return 0, false
}

type Repository interface {
	// Portfolios
	// CreatePortfolio enforces maxPortfolios atomically (count-check + insert inside
	// one transaction, serialized via a row lock) and returns ErrPortfolioLimitReached
	// if the user is already at the limit.
	CreatePortfolio(ctx context.Context, userID, name, description string, maxPortfolios int) (*Portfolio, error)
	ListPortfolios(ctx context.Context, userID string) ([]*Portfolio, error)
	GetPortfolio(ctx context.Context, id string) (*Portfolio, error)
	UpdatePortfolio(ctx context.Context, id string, name, description *string) (*Portfolio, error)
	DeletePortfolio(ctx context.Context, id string) error

	// Positions (scoped to a portfolio)
	// Add enforces maxPositions atomically (count-check + insert inside one
	// transaction, serialized via a row lock) and returns ErrPositionLimitReached if
	// the portfolio is already at the limit.
	Add(ctx context.Context, portfolioID, symbol, name string, shares, avgCost float64, maxPositions int) (*Position, error)
	Remove(ctx context.Context, portfolioID, symbol string) error
	Update(ctx context.Context, portfolioID, symbol string, shares, avgCost *float64) (*Position, error)
	// ListByPortfolioID omits closed holdings (shares = 0) unless includeClosed.
	ListByPortfolioID(ctx context.Context, portfolioID string, includeClosed bool) ([]*Position, error)
	// ListPositionsByUser returns every position across all of the user's portfolios,
	// with PortfolioID set so callers can group by portfolio.
	ListPositionsByUser(ctx context.Context, userID string) ([]*Position, error)
	GetActivity(ctx context.Context, portfolioID string, limit int) ([]*Activity, error)
	// GetActivityByUser returns the newest activity across all of the user's portfolios,
	// with PortfolioID and PortfolioName set on each row.
	GetActivityByUser(ctx context.Context, userID string, limit int) ([]*Activity, error)

	// Transactions (the ledger). Positions are a cache derived from it: every write below
	// locks the portfolio row, replays the symbol's transactions (see Replay), and in the
	// same DB transaction upserts or deletes the position row. A write that would take
	// the holding negative returns ErrInsufficientShares and changes nothing.
	//
	// ListTransactions returns the portfolio's transactions, newest first (trade date,
	// then created_at); symbol "" means all symbols. RunningShares and RealisedPnl are
	// not populated (use Replay). ListTransactionsByUser spans all of the user's
	// portfolios (PortfolioID set), same order.
	ListTransactions(ctx context.Context, portfolioID, symbol string) ([]*Transaction, error)
	ListTransactionsByUser(ctx context.Context, userID string) ([]*Transaction, error)
	// CreateTransaction inserts t (ID/CreatedAt ignored) and returns it with the updated
	// position (Closed=true if it now holds 0 shares). A buy that would open a new
	// position when maxPositions open positions already exist returns
	// ErrPositionLimitReached.
	CreateTransaction(ctx context.Context, portfolioID string, t *Transaction, maxPositions int) (*Transaction, *Position, error)
	// UpdateTransaction applies patch to the transaction txID of portfolioID. The row is
	// re-read and patched inside the locked DB transaction, so a concurrent edit is never
	// overwritten with a stale merge (symbol/name/isOpening are unchanged).
	// ErrTransactionNotFound if missing (or txID is not a valid id). A change that takes a
	// closed symbol back to open when maxPositions open positions already exist returns
	// ErrPositionLimitReached.
	UpdateTransaction(
		ctx context.Context, portfolioID, txID string, patch TransactionPatch, maxPositions int,
	) (*Transaction, *Position, error)
	// DeleteTransaction removes it; if it was the symbol's last transaction the position
	// row is deleted too. ErrTransactionNotFound if missing (or txID is not a valid id).
	// Deleting a sell can reopen a closed symbol; with maxPositions open positions
	// already held that returns ErrPositionLimitReached.
	DeleteTransaction(ctx context.Context, portfolioID, txID string, maxPositions int) error
	GetTransaction(ctx context.Context, portfolioID, txID string) (*Transaction, error)

	// Value snapshots (scoped to a user, across all of their portfolios)
	// ListAllHoldings returns every position of every user, for the snapshot job.
	ListAllHoldings(ctx context.Context) ([]*UserHolding, error)
	// UpsertValueSnapshots records each user's total value for the given day, replacing
	// any value already recorded for that user and day.
	UpsertValueSnapshots(ctx context.Context, date time.Time, valueByUser map[string]float64) error
	// GetValueHistory returns the user's recorded values, oldest first. A nil from
	// means no lower bound.
	GetValueHistory(ctx context.Context, userID string, from *time.Time) ([]*ValuePoint, error)
}
