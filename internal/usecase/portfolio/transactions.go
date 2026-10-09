package portfolio

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
	"github.com/kanitin/stackvest/backend/pkg/logger"
)

// ErrHoldingEditGone is returned by UpdatePosition: holdings are no longer edited
// directly, only through their transactions. Handlers map it to 410 Gone.
var ErrHoldingEditGone = errors.New("Holdings are now edited through their transactions.") //nolint:staticcheck // user-facing message

// TransactionInput is a new buy or sell. Fee is 0 when not given; Date is YYYY-MM-DD.
type TransactionInput struct {
	Symbol   string
	Name     string
	Side     string
	Quantity float64
	Price    float64
	Fee      float64
	Note     *string
	Date     string
}

// TransactionPatch changes some fields of a stored transaction; nil fields are kept.
// The symbol cannot change.
type TransactionPatch = portfoliodomain.TransactionPatch

// TransactionResult is the response of a transaction write: the stored row (with
// RunningShares/RealisedPnl) and the symbol's resulting position.
type TransactionResult struct {
	Transaction *portfoliodomain.Transaction `json:"transaction"`
	Position    *portfoliodomain.Position    `json:"position"`
}

// maxAmount is the exclusive upper bound of a quantity, price or fee: the NUMERIC(20,8)
// columns cannot store 1e12 or more.
const maxAmount = 1e12

// validateTransaction checks the fields the DB would otherwise reject (and Replay would
// refuse later): side, quantity > 0, price and fee >= 0, a parseable non-future date.
func validateTransaction(t *portfoliodomain.Transaction, now time.Time) error {
	if t.Side != portfoliodomain.SideBuy && t.Side != portfoliodomain.SideSell {
		return fmt.Errorf("%w: side must be buy or sell", portfoliodomain.ErrInvalidTransaction)
	}
	if !(t.Quantity > 0) {
		return fmt.Errorf("%w: quantity must be greater than 0", portfoliodomain.ErrInvalidTransaction)
	}
	if !(t.Price >= 0) {
		return fmt.Errorf("%w: price must not be negative", portfoliodomain.ErrInvalidTransaction)
	}
	if !(t.Fee >= 0) {
		return fmt.Errorf("%w: fee must not be negative", portfoliodomain.ErrInvalidTransaction)
	}
	// NUMERIC(20,8) holds at most 12 integer digits; larger values (or +Inf) would fail
	// in the database as a 500 instead of a clean 400.
	if t.Quantity >= maxAmount {
		return fmt.Errorf("%w: quantity must be less than %g", portfoliodomain.ErrInvalidTransaction, maxAmount)
	}
	if t.Price >= maxAmount {
		return fmt.Errorf("%w: price must be less than %g", portfoliodomain.ErrInvalidTransaction, maxAmount)
	}
	if t.Fee >= maxAmount {
		return fmt.Errorf("%w: fee must be less than %g", portfoliodomain.ErrInvalidTransaction, maxAmount)
	}
	return portfoliodomain.ValidateDate(t.Date, now)
}

func (uc *UseCase) CreateTransaction(
	ctx context.Context, email, portfolioID string, in TransactionInput,
) (*TransactionResult, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	t := &portfoliodomain.Transaction{
		PortfolioID: portfolioID,
		Symbol:      strings.ToUpper(strings.TrimSpace(in.Symbol)),
		Name:        strings.TrimSpace(in.Name),
		Side:        in.Side,
		Quantity:    in.Quantity,
		Price:       in.Price,
		Fee:         in.Fee,
		Note:        in.Note,
		Date:        in.Date,
	}
	if t.Symbol == "" || t.Name == "" {
		return nil, fmt.Errorf("%w: symbol and name are required", portfoliodomain.ErrInvalidTransaction)
	}
	if err := validateTransaction(t, time.Now()); err != nil {
		return nil, err
	}
	created, pos, err := uc.repo.CreateTransaction(ctx, portfolioID, t, uc.maxPositions)
	if err != nil {
		return nil, err
	}
	return uc.transactionResult(ctx, created, pos), nil
}

func (uc *UseCase) UpdateTransaction(
	ctx context.Context, email, portfolioID, txID string, patch TransactionPatch,
) (*TransactionResult, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, err
	}
	t, err := uc.repo.GetTransaction(ctx, portfolioID, txID)
	if err != nil {
		return nil, err
	}
	// The merge below only validates the patch against the row as read now. The repo
	// re-applies the patch to the row it re-reads under the portfolio lock, so a
	// concurrent edit of other fields is kept rather than overwritten. Every field is
	// validated on its own, so the re-merged row is valid as well.
	merged := *t
	merged.PortfolioID = portfolioID
	patch.Apply(&merged)
	// A date that was not touched is already stored and valid; only a new one is checked
	// against today, so editing the fee of an old row never fails on the date.
	if patch.Date != nil {
		if err := portfoliodomain.ValidateDate(merged.Date, time.Now()); err != nil {
			return nil, err
		}
	}
	probe := merged
	probe.Date = t.Date
	if err := validateTransaction(&probe, time.Now()); err != nil {
		return nil, err
	}

	updated, pos, err := uc.repo.UpdateTransaction(ctx, portfolioID, txID, patch, uc.maxPositions)
	if err != nil {
		return nil, err
	}
	return uc.transactionResult(ctx, updated, pos), nil
}

func (uc *UseCase) DeleteTransaction(ctx context.Context, email, portfolioID, txID string) error {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return err
	}
	return uc.repo.DeleteTransaction(ctx, portfolioID, txID, uc.maxPositions)
}

// ListTransactions returns one page of the portfolio's transactions (newest first),
// optionally for a single symbol, plus the total count. RunningShares and RealisedPnl are
// derived by replaying each symbol's full history, so they do not depend on the page.
func (uc *UseCase) ListTransactions(
	ctx context.Context, email, portfolioID, symbol string, page, size int,
) ([]*portfoliodomain.Transaction, int, error) {
	if _, err := uc.ownedPortfolio(ctx, email, portfolioID); err != nil {
		return nil, 0, err
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	txs, err := uc.repo.ListTransactions(ctx, portfolioID, symbol)
	if err != nil {
		return nil, 0, err
	}
	fillLedgerFigures(ctx, txs)

	total := len(txs)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	return txs[start:end], total, nil
}

// fillLedgerFigures replays each symbol's transactions and copies RunningShares (and
// RealisedPnl for sells) onto the matching rows. A symbol whose history cannot be
// replayed (it should not happen: writes are replay-checked) is logged and left zeroed.
func fillLedgerFigures(ctx context.Context, txs []*portfoliodomain.Transaction) {
	bySymbol := make(map[string][]*portfoliodomain.Transaction)
	for _, t := range txs {
		bySymbol[t.Symbol] = append(bySymbol[t.Symbol], t)
	}
	for symbol, group := range bySymbol {
		res, err := portfoliodomain.Replay(group)
		if err != nil {
			zap.L().Warn("failed to replay symbol ledger", logger.RequestID(ctx), zap.String("symbol", symbol), zap.Error(err))
			continue
		}
		rows := make(map[string]portfoliodomain.LedgerRow, len(res.Rows))
		for _, row := range res.Rows {
			rows[row.TxID] = row
		}
		for _, t := range group {
			row, ok := rows[t.ID]
			if !ok {
				continue
			}
			t.RunningShares = row.RunningShares
			if t.Side == portfoliodomain.SideSell {
				pnl := row.RealisedPnl
				t.RealisedPnl = &pnl
			}
		}
	}
}

// transactionResult enriches the position with its live value and P&L (best effort).
func (uc *UseCase) transactionResult(
	ctx context.Context, t *portfoliodomain.Transaction, pos *portfoliodomain.Position,
) *TransactionResult {
	if pos != nil {
		uc.enrichPositions(ctx, []*portfoliodomain.Position{pos})
	}
	return &TransactionResult{Transaction: t, Position: pos}
}
