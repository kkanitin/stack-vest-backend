package portfolio

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
)

// pgInvalidTextRepresentation is the SQLSTATE Postgres raises when a value cannot be
// cast to the column type, e.g. a non-UUID path id compared with a uuid column.
const pgInvalidTextRepresentation = "22P02"

// isMalformedID reports whether err is Postgres refusing an id that is not a UUID. Such an
// id cannot match any row, so callers treat it as "not found" rather than a 500.
func isMalformedID(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgInvalidTextRepresentation
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// --- Portfolios ---

func (r *PostgresRepository) CreatePortfolio(
	ctx context.Context, userID, name, description string, maxPortfolios int,
) (*portfoliodomain.Portfolio, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Lock the user row so a concurrent CreatePortfolio for the same user blocks
	// here until this transaction commits or rolls back — the count check below
	// can no longer race with another transaction's insert. The user row always
	// exists (unlike a to-be-created portfolio row), so this holds even when the
	// user currently has zero portfolios.
	if _, err := tx.Exec(ctx, `SELECT id FROM stackvest.users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return nil, err
	}

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM stackvest.portfolios WHERE user_id = $1`, userID,
	).Scan(&count); err != nil {
		return nil, err
	}
	if count >= maxPortfolios {
		return nil, portfoliodomain.ErrPortfolioLimitReached
	}

	var p portfoliodomain.Portfolio
	err = tx.QueryRow(ctx,
		`INSERT INTO stackvest.portfolios (user_id, name, description)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, name, description, created_at, updated_at`,
		userID, name, description,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PostgresRepository) ListPortfolios(ctx context.Context, userID string) ([]*portfoliodomain.Portfolio, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, name, description, created_at, updated_at
		 FROM stackvest.portfolios
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var portfolios []*portfoliodomain.Portfolio
	for rows.Next() {
		var p portfoliodomain.Portfolio
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		portfolios = append(portfolios, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if portfolios == nil {
		portfolios = []*portfoliodomain.Portfolio{}
	}
	return portfolios, nil
}

func (r *PostgresRepository) GetPortfolio(ctx context.Context, id string) (*portfoliodomain.Portfolio, error) {
	var p portfoliodomain.Portfolio
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, name, description, created_at, updated_at
		 FROM stackvest.portfolios
		 WHERE id = $1`,
		id,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) || isMalformedID(err) {
		return nil, portfoliodomain.ErrPortfolioNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PostgresRepository) UpdatePortfolio(ctx context.Context, id string, name, description *string) (*portfoliodomain.Portfolio, error) {
	var p portfoliodomain.Portfolio
	err := r.pool.QueryRow(ctx,
		`UPDATE stackvest.portfolios
		 SET name        = COALESCE($2, name),
		     description = COALESCE($3, description),
		     updated_at  = NOW()
		 WHERE id = $1
		 RETURNING id, user_id, name, description, created_at, updated_at`,
		id, name, description,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) || isMalformedID(err) {
		return nil, portfoliodomain.ErrPortfolioNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PostgresRepository) DeletePortfolio(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM stackvest.portfolios WHERE id = $1`,
		id,
	)
	if isMalformedID(err) {
		return portfoliodomain.ErrPortfolioNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return portfoliodomain.ErrPortfolioNotFound
	}
	return nil
}

// --- Positions ---
//
// portfolio_positions is a cache derived from portfolio_transactions: every ledger write
// below locks the portfolio row, replays the affected symbol's transactions and rewrites
// the cache row in the same DB transaction. shares = 0 is a closed holding (kept so its
// realised P&L stays visible); every reader that counts or values holdings filters on
// shares > 0.

const positionCols = `id, portfolio_id, symbol, name, shares, avg_cost, realised_pnl, added_at`

type rowScanner interface{ Scan(dest ...any) error }

func scanPosition(row rowScanner, pos *portfoliodomain.Position) error {
	if err := row.Scan(
		&pos.ID, &pos.PortfolioID, &pos.Symbol, &pos.Name, &pos.Shares, &pos.AvgCost, &pos.RealisedPnl, &pos.AddedAt,
	); err != nil {
		return err
	}
	pos.CostBasis = portfoliodomain.Round8(pos.Shares * pos.AvgCost)
	pos.Closed = pos.Shares <= 0
	return nil
}

// Add is the legacy "add a holding" entry: it records a buy dated today (UTC).
func (r *PostgresRepository) Add(
	ctx context.Context, portfolioID, symbol, name string, shares, avgCost float64, maxPositions int,
) (*portfoliodomain.Position, error) {
	_, pos, err := r.CreateTransaction(ctx, portfolioID, &portfoliodomain.Transaction{
		Symbol:   symbol,
		Name:     name,
		Side:     portfoliodomain.SideBuy,
		Quantity: shares,
		Price:    avgCost,
		Date:     time.Now().UTC().Format(portfoliodomain.DateLayout),
	}, maxPositions)
	return pos, err
}

// Remove deletes the holding and its whole transaction history (the "entered by mistake"
// path). portfolio_transactions only cascades from the portfolio, so it is deleted
// explicitly here.
func (r *PostgresRepository) Remove(ctx context.Context, portfolioID, symbol string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := lockPortfolio(ctx, tx, portfolioID); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx,
		`DELETE FROM stackvest.portfolio_positions WHERE portfolio_id = $1 AND symbol = $2`,
		portfolioID, symbol,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return portfoliodomain.ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM stackvest.portfolio_transactions WHERE portfolio_id = $1 AND symbol = $2`,
		portfolioID, symbol,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// errHoldingEditGone is returned by the legacy Update: holdings are no longer typed in
// directly; they change only through their transactions.
var errHoldingEditGone = errors.New("holdings are edited through their transactions")

func (r *PostgresRepository) Update(context.Context, string, string, *float64, *float64) (*portfoliodomain.Position, error) {
	return nil, errHoldingEditGone
}

func (r *PostgresRepository) ListByPortfolioID(ctx context.Context, portfolioID string, includeClosed bool) ([]*portfoliodomain.Position, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+positionCols+`
		 FROM stackvest.portfolio_positions
		 WHERE portfolio_id = $1 AND ($2::boolean OR shares > 0)
		 ORDER BY added_at DESC`,
		portfolioID, includeClosed,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	positions := []*portfoliodomain.Position{}
	for rows.Next() {
		var pos portfoliodomain.Position
		if err := scanPosition(rows, &pos); err != nil {
			return nil, err
		}
		positions = append(positions, &pos)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return positions, nil
}

// ListPositionsByUser returns open positions only (shares > 0). It backs the portfolio
// list (assetCount/value), the summaries and the dividend calendar's current holdings.
func (r *PostgresRepository) ListPositionsByUser(ctx context.Context, userID string) ([]*portfoliodomain.Position, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT pp.id, pp.portfolio_id, pp.symbol, pp.name, pp.shares, pp.avg_cost, pp.realised_pnl, pp.added_at
		 FROM stackvest.portfolio_positions pp
		 JOIN stackvest.portfolios p ON p.id = pp.portfolio_id
		 WHERE p.user_id = $1 AND pp.shares > 0
		 ORDER BY pp.added_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	positions := []*portfoliodomain.Position{}
	for rows.Next() {
		var pos portfoliodomain.Position
		if err := scanPosition(rows, &pos); err != nil {
			return nil, err
		}
		positions = append(positions, &pos)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return positions, nil
}

// --- Activity (read from the ledger) ---

const activityCols = `t.id, t.symbol, t.side, t.is_opening, t.quantity, t.price, t.created_at`

// buildActivity maps a ledger row into the Activity shape the feed has always used.
func buildActivity(id, symbol, side string, opening bool, qty, price float64, at time.Time) *portfoliodomain.Activity {
	act := &portfoliodomain.Activity{ID: id, Symbol: symbol, Timestamp: at}
	q := strconv.FormatFloat(qty, 'f', -1, 64)
	switch {
	case opening:
		act.Label = "Opening balance"
		act.Detail = fmt.Sprintf("%s %s @ $%.2f", q, symbol, price)
	case side == portfoliodomain.SideSell:
		act.Label = fmt.Sprintf("Sold %s %s", q, symbol)
		act.Detail = fmt.Sprintf("@ $%.2f", price)
	default:
		act.Label = fmt.Sprintf("Bought %s %s", q, symbol)
		act.Detail = fmt.Sprintf("@ $%.2f", price)
	}
	if side == portfoliodomain.SideSell {
		act.Tone, act.Badge = "negative", "SELL"
	} else {
		act.Tone, act.Badge = "positive", "BUY"
	}
	return act
}

func (r *PostgresRepository) GetActivity(ctx context.Context, portfolioID string, limit int) ([]*portfoliodomain.Activity, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+activityCols+`
		 FROM stackvest.portfolio_transactions t
		 WHERE t.portfolio_id = $1
		 ORDER BY t.created_at DESC
		 LIMIT $2`,
		portfolioID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activities := []*portfoliodomain.Activity{}
	for rows.Next() {
		var (
			id, symbol, side string
			opening          bool
			qty, price       float64
			at               time.Time
		)
		if err := rows.Scan(&id, &symbol, &side, &opening, &qty, &price, &at); err != nil {
			return nil, err
		}
		activities = append(activities, buildActivity(id, symbol, side, opening, qty, price, at))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return activities, nil
}

func (r *PostgresRepository) GetActivityByUser(ctx context.Context, userID string, limit int) ([]*portfoliodomain.Activity, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+activityCols+`, p.id, p.name
		 FROM stackvest.portfolio_transactions t
		 JOIN stackvest.portfolios p ON p.id = t.portfolio_id
		 WHERE p.user_id = $1
		 ORDER BY t.created_at DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activities := []*portfoliodomain.Activity{}
	for rows.Next() {
		var (
			id, symbol, side string
			opening          bool
			qty, price       float64
			at               time.Time
			pfID, pfName     string
		)
		if err := rows.Scan(&id, &symbol, &side, &opening, &qty, &price, &at, &pfID, &pfName); err != nil {
			return nil, err
		}
		act := buildActivity(id, symbol, side, opening, qty, price, at)
		act.PortfolioID, act.PortfolioName = pfID, pfName
		activities = append(activities, act)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return activities, nil
}

// --- Value snapshots ---

func (r *PostgresRepository) ListAllHoldings(ctx context.Context) ([]*portfoliodomain.UserHolding, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT p.user_id, pp.symbol, pp.shares
		 FROM stackvest.portfolio_positions pp
		 JOIN stackvest.portfolios p ON p.id = pp.portfolio_id
		 WHERE pp.shares > 0`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	holdings := []*portfoliodomain.UserHolding{}
	for rows.Next() {
		var h portfoliodomain.UserHolding
		if err := rows.Scan(&h.UserID, &h.Symbol, &h.Shares); err != nil {
			return nil, err
		}
		holdings = append(holdings, &h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return holdings, nil
}

func (r *PostgresRepository) UpsertValueSnapshots(ctx context.Context, date time.Time, valueByUser map[string]float64) error {
	if len(valueByUser) == 0 {
		return nil
	}
	// A batch runs in one implicit transaction, so a day's snapshot is written for
	// every user or for none.
	batch := &pgx.Batch{}
	for userID, value := range valueByUser {
		batch.Queue(
			`INSERT INTO stackvest.user_value_snapshots (user_id, snapshot_date, value_usd)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (user_id, snapshot_date)
			 DO UPDATE SET value_usd = EXCLUDED.value_usd, updated_at = NOW()`,
			userID, date, value,
		)
	}
	return r.pool.SendBatch(ctx, batch).Close()
}

func (r *PostgresRepository) GetValueHistory(ctx context.Context, userID string, from *time.Time) ([]*portfoliodomain.ValuePoint, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT to_char(snapshot_date, 'YYYY-MM-DD'), value_usd
		 FROM stackvest.user_value_snapshots
		 WHERE user_id = $1 AND ($2::date IS NULL OR snapshot_date >= $2::date)
		 ORDER BY snapshot_date`,
		userID, from,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := []*portfoliodomain.ValuePoint{}
	for rows.Next() {
		var p portfoliodomain.ValuePoint
		if err := rows.Scan(&p.Date, &p.Value); err != nil {
			return nil, err
		}
		points = append(points, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return points, nil
}

var _ portfoliodomain.Repository = (*PostgresRepository)(nil)

// --- Transactions (ledger) ---

const txCols = `id, portfolio_id, symbol, name, side, quantity, price, fee, note,
	to_char(trade_date, 'YYYY-MM-DD'), is_opening, created_at`

func scanTransaction(row rowScanner) (*portfoliodomain.Transaction, error) {
	var t portfoliodomain.Transaction
	if err := row.Scan(
		&t.ID, &t.PortfolioID, &t.Symbol, &t.Name, &t.Side, &t.Quantity, &t.Price, &t.Fee, &t.Note,
		&t.Date, &t.IsOpening, &t.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

func collectTransactions(rows pgx.Rows) ([]*portfoliodomain.Transaction, error) {
	defer rows.Close()
	txs := []*portfoliodomain.Transaction{}
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		txs = append(txs, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return txs, nil
}

// parseTradeDate turns a YYYY-MM-DD trade date into a time for the DATE column.
func parseTradeDate(date string) (time.Time, error) {
	d, err := time.Parse(portfoliodomain.DateLayout, date)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date must be YYYY-MM-DD", portfoliodomain.ErrInvalidTransaction)
	}
	return d, nil
}

// lockPortfolio takes the portfolio row FOR UPDATE so concurrent ledger writes on the
// same portfolio serialize (see CreatePortfolio for why the parent row is locked).
func lockPortfolio(ctx context.Context, tx pgx.Tx, portfolioID string) error {
	tag, err := tx.Exec(ctx, `SELECT id FROM stackvest.portfolios WHERE id = $1 FOR UPDATE`, portfolioID)
	if isMalformedID(err) {
		return portfoliodomain.ErrPortfolioNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return portfoliodomain.ErrPortfolioNotFound
	}
	return nil
}

// symbolTransactions loads every transaction of one symbol in the portfolio, oldest first.
func symbolTransactions(ctx context.Context, tx pgx.Tx, portfolioID, symbol string) ([]*portfoliodomain.Transaction, error) {
	rows, err := tx.Query(ctx,
		`SELECT `+txCols+`
		 FROM stackvest.portfolio_transactions
		 WHERE portfolio_id = $1 AND symbol = $2
		 ORDER BY trade_date, created_at`,
		portfolioID, symbol,
	)
	if err != nil {
		return nil, err
	}
	return collectTransactions(rows)
}

// applyRows copies the replay's per-row figures onto t (RealisedPnl only for sells).
func applyRows(t *portfoliodomain.Transaction, res portfoliodomain.LedgerResult) {
	for _, row := range res.Rows {
		if row.TxID != t.ID {
			continue
		}
		t.RunningShares = row.RunningShares
		if t.Side == portfoliodomain.SideSell {
			pnl := row.RealisedPnl
			t.RealisedPnl = &pnl
		}
		return
	}
}

// syncPosition rewrites the portfolio_positions cache row for symbol from txs (all of the
// symbol's transactions after the change). It returns the replay error (e.g.
// ErrInsufficientShares) unchanged so the caller's DB transaction rolls back. With no
// transactions left the cache row is deleted and the returned position is nil.
// added_at is set on first insert and refreshed when a closed holding reopens.
func syncPosition(
	ctx context.Context, tx pgx.Tx, portfolioID, symbol, name string, txs []*portfoliodomain.Transaction,
) (*portfoliodomain.Position, portfoliodomain.LedgerResult, error) {
	if len(txs) == 0 {
		_, err := tx.Exec(ctx,
			`DELETE FROM stackvest.portfolio_positions WHERE portfolio_id = $1 AND symbol = $2`,
			portfolioID, symbol,
		)
		return nil, portfoliodomain.LedgerResult{}, err
	}

	res, err := portfoliodomain.Replay(txs)
	if err != nil {
		return nil, res, err
	}

	var pos portfoliodomain.Position
	err = scanPosition(tx.QueryRow(ctx,
		`INSERT INTO stackvest.portfolio_positions AS pp (portfolio_id, symbol, name, shares, avg_cost, realised_pnl)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (portfolio_id, symbol) DO UPDATE SET
		     name         = EXCLUDED.name,
		     added_at     = CASE WHEN pp.shares = 0 AND EXCLUDED.shares > 0
		                         THEN NOW() ELSE pp.added_at END,
		     shares       = EXCLUDED.shares,
		     avg_cost     = EXCLUDED.avg_cost,
		     realised_pnl = EXCLUDED.realised_pnl
		 RETURNING `+positionCols,
		portfolioID, symbol, name,
		portfoliodomain.Round8(res.Shares), portfoliodomain.Round8(res.AvgCost), portfoliodomain.Round8(res.RealisedPnl),
	), &pos)
	if err != nil {
		return nil, res, err
	}
	return &pos, res, nil
}

// checkPositionLimit enforces the open-position limit for a ledger write that moved
// symbol from `before` to `after` shares. Only a change that makes a not-currently-open
// symbol open counts (a new symbol, or reopening a closed one, whether by a buy, an edit
// or deleting a sell). The other open positions are counted under the portfolio lock, so
// concurrent writes cannot both slip under the limit. Call it after syncPosition so a
// failure rolls the whole write back.
func checkPositionLimit(
	ctx context.Context, tx pgx.Tx, portfolioID, symbol string, before, after float64, maxPositions int,
) error {
	if before > 0 || after <= 0 {
		return nil
	}
	var open int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM stackvest.portfolio_positions
		 WHERE portfolio_id = $1 AND shares > 0 AND symbol <> $2`,
		portfolioID, symbol,
	).Scan(&open); err != nil {
		return err
	}
	if open >= maxPositions {
		return portfoliodomain.ErrPositionLimitReached
	}
	return nil
}

func (r *PostgresRepository) ListTransactions(ctx context.Context, portfolioID, symbol string) ([]*portfoliodomain.Transaction, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+txCols+`
		 FROM stackvest.portfolio_transactions
		 WHERE portfolio_id = $1 AND ($2::text = '' OR symbol = $2::text)
		 ORDER BY trade_date DESC, created_at DESC`,
		portfolioID, symbol,
	)
	if err != nil {
		return nil, err
	}
	return collectTransactions(rows)
}

func (r *PostgresRepository) ListTransactionsByUser(ctx context.Context, userID string) ([]*portfoliodomain.Transaction, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT t.id, t.portfolio_id, t.symbol, t.name, t.side, t.quantity, t.price, t.fee, t.note,
		        to_char(t.trade_date, 'YYYY-MM-DD'), t.is_opening, t.created_at
		 FROM stackvest.portfolio_transactions t
		 JOIN stackvest.portfolios p ON p.id = t.portfolio_id
		 WHERE p.user_id = $1
		 ORDER BY t.trade_date DESC, t.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	return collectTransactions(rows)
}

func (r *PostgresRepository) GetTransaction(ctx context.Context, portfolioID, txID string) (*portfoliodomain.Transaction, error) {
	t, err := scanTransaction(r.pool.QueryRow(ctx,
		`SELECT `+txCols+` FROM stackvest.portfolio_transactions WHERE id = $1 AND portfolio_id = $2`,
		txID, portfolioID,
	))
	if errors.Is(err, pgx.ErrNoRows) || isMalformedID(err) {
		return nil, portfoliodomain.ErrTransactionNotFound
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *PostgresRepository) CreateTransaction(
	ctx context.Context, portfolioID string, t *portfoliodomain.Transaction, maxPositions int,
) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := lockPortfolio(ctx, tx, portfolioID); err != nil {
		return nil, nil, err
	}

	existing, err := symbolTransactions(ctx, tx, portfolioID, t.Symbol)
	if err != nil {
		return nil, nil, err
	}
	// The stored ledger is always valid, so this replay cannot fail; it tells whether the
	// symbol is an open position before the new row.
	before, err := portfoliodomain.Replay(existing)
	if err != nil {
		return nil, nil, err
	}

	tradeDate, err := parseTradeDate(t.Date)
	if err != nil {
		return nil, nil, err
	}
	created, err := scanTransaction(tx.QueryRow(ctx,
		`INSERT INTO stackvest.portfolio_transactions
		     (portfolio_id, symbol, name, side, quantity, price, fee, note, trade_date, is_opening)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::date, $10)
		 RETURNING `+txCols,
		portfolioID, t.Symbol, t.Name, t.Side, t.Quantity, t.Price, t.Fee, t.Note, tradeDate, t.IsOpening,
	))
	if err != nil {
		return nil, nil, err
	}

	all := append(existing, created)
	pos, res, err := syncPosition(ctx, tx, portfolioID, created.Symbol, created.Name, all)
	if err != nil {
		return nil, nil, err
	}

	if err := checkPositionLimit(ctx, tx, portfolioID, created.Symbol, before.Shares, res.Shares, maxPositions); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	applyRows(created, res)
	return created, pos, nil
}

func (r *PostgresRepository) UpdateTransaction(
	ctx context.Context, portfolioID, txID string, patch portfoliodomain.TransactionPatch, maxPositions int,
) (*portfoliodomain.Transaction, *portfoliodomain.Position, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := lockPortfolio(ctx, tx, portfolioID); err != nil {
		return nil, nil, err
	}

	// Re-read the row under the lock and patch that, so an edit that committed after the
	// use case's own read is merged with this one instead of being overwritten.
	current, err := scanTransaction(tx.QueryRow(ctx,
		`SELECT `+txCols+` FROM stackvest.portfolio_transactions WHERE id = $1 AND portfolio_id = $2`,
		txID, portfolioID,
	))
	if errors.Is(err, pgx.ErrNoRows) || isMalformedID(err) {
		return nil, nil, portfoliodomain.ErrTransactionNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	patch.Apply(current)

	existing, err := symbolTransactions(ctx, tx, portfolioID, current.Symbol)
	if err != nil {
		return nil, nil, err
	}
	// The stored ledger is always valid, so this replay cannot fail; it tells whether the
	// symbol was open before the edit.
	before, err := portfoliodomain.Replay(existing)
	if err != nil {
		return nil, nil, err
	}

	tradeDate, err := parseTradeDate(current.Date)
	if err != nil {
		return nil, nil, err
	}
	updated, err := scanTransaction(tx.QueryRow(ctx,
		`UPDATE stackvest.portfolio_transactions
		 SET side = $3, quantity = $4, price = $5, fee = $6, note = $7, trade_date = $8::date
		 WHERE id = $1 AND portfolio_id = $2
		 RETURNING `+txCols,
		current.ID, portfolioID, current.Side, current.Quantity, current.Price, current.Fee, current.Note, tradeDate,
	))
	if err != nil {
		return nil, nil, err
	}

	all, err := symbolTransactions(ctx, tx, portfolioID, updated.Symbol)
	if err != nil {
		return nil, nil, err
	}
	pos, res, err := syncPosition(ctx, tx, portfolioID, updated.Symbol, updated.Name, all)
	if err != nil {
		return nil, nil, err
	}
	// An edit (say a sell turned into a buy) can reopen a closed symbol.
	if err := checkPositionLimit(ctx, tx, portfolioID, updated.Symbol, before.Shares, res.Shares, maxPositions); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	applyRows(updated, res)
	return updated, pos, nil
}

func (r *PostgresRepository) DeleteTransaction(ctx context.Context, portfolioID, txID string, maxPositions int) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := lockPortfolio(ctx, tx, portfolioID); err != nil {
		return err
	}

	var symbol, name string
	err = tx.QueryRow(ctx,
		`SELECT symbol, name FROM stackvest.portfolio_transactions WHERE id = $1 AND portfolio_id = $2`,
		txID, portfolioID,
	).Scan(&symbol, &name)
	if errors.Is(err, pgx.ErrNoRows) || isMalformedID(err) {
		return portfoliodomain.ErrTransactionNotFound
	}
	if err != nil {
		return err
	}

	existing, err := symbolTransactions(ctx, tx, portfolioID, symbol)
	if err != nil {
		return err
	}
	before, err := portfoliodomain.Replay(existing)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM stackvest.portfolio_transactions WHERE id = $1 AND portfolio_id = $2`,
		txID, portfolioID,
	); err != nil {
		return err
	}

	remaining, err := symbolTransactions(ctx, tx, portfolioID, symbol)
	if err != nil {
		return err
	}
	// With no transactions left the position row is deleted; otherwise the replay may
	// refuse (a later sell is now uncovered) and the delete rolls back.
	_, res, err := syncPosition(ctx, tx, portfolioID, symbol, name, remaining)
	if err != nil {
		return err
	}
	// Deleting a sell can reopen a closed symbol.
	if err := checkPositionLimit(ctx, tx, portfolioID, symbol, before.Shares, res.Shares, maxPositions); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
