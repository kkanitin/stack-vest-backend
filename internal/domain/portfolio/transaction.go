package portfolio

import (
	"errors"
	"fmt"
	"time"
)

const (
	SideBuy  = "buy"
	SideSell = "sell"

	// DateLayout is the wire and storage format of a trade date.
	DateLayout = "2006-01-02"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
	// ErrFutureDate is returned for a trade date after today (UTC).
	ErrFutureDate = errors.New("trade date cannot be in the future")
	// ErrInvalidTransaction is returned for a malformed row (bad side, quantity <= 0,
	// unparseable date). Handlers map it to 400.
	ErrInvalidTransaction = errors.New("invalid transaction")
)

// ErrInsufficientShares is returned when a sell (new, edited, or exposed by deleting or
// editing an earlier buy) would take the holding below zero. Handlers map it to 409.
type ErrInsufficientShares struct {
	Symbol string
	Date   string  // YYYY-MM-DD of the offending sell
	Held   float64 // shares held just before that sell
	Sold   float64
}

func (e ErrInsufficientShares) Error() string {
	return fmt.Sprintf("This would leave you selling %g %s on %s when you held %g.", e.Sold, e.Symbol, e.Date, e.Held)
}

// Transaction is one buy or sell. Date is the trade date (YYYY-MM-DD). Opening is true
// for the entry created from a pre-ledger holding. RunningShares and RealisedPnl are
// derived by Replay and filled in by the use case (RealisedPnl only for sells).
type Transaction struct {
	ID          string    `json:"id"`
	PortfolioID string    `json:"-"`
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	Side        string    `json:"side"`
	Quantity    float64   `json:"quantity"`
	Price       float64   `json:"price"`
	Fee         float64   `json:"fee"`
	Note        *string   `json:"note,omitempty"`
	Date        string    `json:"date"`
	IsOpening   bool      `json:"isOpening"`
	CreatedAt   time.Time `json:"-"`

	RunningShares float64  `json:"runningShares"`
	RealisedPnl   *float64 `json:"realisedPnl,omitempty"`
}

// LedgerRow is the per-transaction output of Replay, in replay order. RealisedPnl is 0
// for buys.
type LedgerRow struct {
	TxID          string
	RunningShares float64
	RealisedPnl   float64
}

// LedgerResult is the holding derived from a symbol's transactions. Shares == 0 means
// the holding is closed (AvgCost is then 0).
type LedgerResult struct {
	Shares      float64
	AvgCost     float64
	RealisedPnl float64
	Rows        []LedgerRow
}

// TransactionPatch changes some fields of a stored transaction; nil fields are kept.
// The symbol cannot change.
type TransactionPatch struct {
	Side     *string
	Quantity *float64
	Price    *float64
	Fee      *float64
	Note     *string
	Date     *string
}

// Apply copies the set fields of p onto t.
func (p TransactionPatch) Apply(t *Transaction) {
	if p.Side != nil {
		t.Side = *p.Side
	}
	if p.Quantity != nil {
		t.Quantity = *p.Quantity
	}
	if p.Price != nil {
		t.Price = *p.Price
	}
	if p.Fee != nil {
		t.Fee = *p.Fee
	}
	if p.Note != nil {
		t.Note = p.Note
	}
	if p.Date != nil {
		t.Date = *p.Date
	}
}
