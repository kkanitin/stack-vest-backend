package portfolio

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Round8 rounds to 8 decimal places, the precision of the NUMERIC(20,8) columns.
func Round8(v float64) float64 { return math.Round(v*1e8) / 1e8 }

// ValidateDate reports ErrInvalidTransaction for a date that is not YYYY-MM-DD and
// ErrFutureDate for one after the UTC calendar day of now.
func ValidateDate(date string, now time.Time) error {
	d, err := time.Parse(DateLayout, date)
	if err != nil {
		return fmt.Errorf("%w: date must be YYYY-MM-DD", ErrInvalidTransaction)
	}
	if d.After(now.UTC().Truncate(24 * time.Hour)) {
		return ErrFutureDate
	}
	return nil
}

// TransactionBefore is the ledger order: by trade date, then buys before sells on the
// same day, then creation time. Replay and every other consumer of the ledger (such as
// the daily value series) must use it so they agree on same-day order.
func TransactionBefore(a, b *Transaction) bool {
	if a.Date != b.Date {
		return a.Date < b.Date
	}
	if a.Side != b.Side {
		return a.Side == SideBuy
	}
	return a.CreatedAt.Before(b.CreatedAt)
}

// Replay walks one symbol's transactions and derives the holding. Rows are ordered by
// (Date, buys before sells, CreatedAt), so the input order does not matter; ties keep
// input order. It returns ErrInsufficientShares if any sell exceeds the shares held at
// that point, and ErrInvalidTransaction for a bad side or a non-positive quantity.
// Pure function: it does not mutate txs.
//
// Rules: a buy adds qty*price+fee to cost; a sell realises (price-avg)*qty-fee and
// leaves the average cost unchanged; selling to zero resets cost, so a later buy starts
// a fresh average. Shares, average cost and realised P&L are rounded to 8 decimals.
func Replay(txs []*Transaction) (LedgerResult, error) {
	ordered := make([]*Transaction, len(txs))
	copy(ordered, txs)
	sort.SliceStable(ordered, func(i, j int) bool { return TransactionBefore(ordered[i], ordered[j]) })

	var shares, cost, realised float64
	res := LedgerResult{Rows: make([]LedgerRow, 0, len(ordered))}
	for _, t := range ordered {
		if t.Quantity <= 0 || math.IsNaN(t.Quantity) || t.Price < 0 || t.Fee < 0 {
			return LedgerResult{}, fmt.Errorf("%w: quantity must be > 0 and price/fee >= 0", ErrInvalidTransaction)
		}
		row := LedgerRow{TxID: t.ID}
		switch t.Side {
		case SideBuy:
			cost += t.Quantity*t.Price + t.Fee
			shares = Round8(shares + t.Quantity)
		case SideSell:
			if t.Quantity > shares {
				return LedgerResult{}, ErrInsufficientShares{Symbol: t.Symbol, Date: t.Date, Held: shares, Sold: t.Quantity}
			}
			avg := cost / shares // shares >= t.Quantity > 0
			row.RealisedPnl = Round8((t.Price-avg)*t.Quantity - t.Fee)
			realised += row.RealisedPnl
			shares = Round8(shares - t.Quantity)
			if shares <= 0 {
				shares, cost = 0, 0
			} else {
				cost = avg * shares
			}
		default:
			return LedgerResult{}, fmt.Errorf("%w: side must be buy or sell", ErrInvalidTransaction)
		}
		row.RunningShares = shares
		res.Rows = append(res.Rows, row)
	}

	res.Shares = shares
	if shares > 0 {
		res.AvgCost = Round8(cost / shares)
	}
	res.RealisedPnl = Round8(realised)
	return res, nil
}
