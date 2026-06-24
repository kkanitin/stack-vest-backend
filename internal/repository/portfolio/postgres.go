package portfolio

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	portfoliodomain "github.com/kanitin/stackvest/backend/internal/domain/portfolio"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// --- Portfolios ---

func (r *PostgresRepository) CreatePortfolio(ctx context.Context, userID, name, description string) (*portfoliodomain.Portfolio, error) {
	var p portfoliodomain.Portfolio
	err := r.pool.QueryRow(ctx,
		`INSERT INTO stackvest.portfolios (user_id, name, description)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, name, description, created_at, updated_at`,
		userID, name, description,
	).Scan(&p.ID, &p.UserID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
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
	if errors.Is(err, pgx.ErrNoRows) {
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
	if errors.Is(err, pgx.ErrNoRows) {
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
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return portfoliodomain.ErrPortfolioNotFound
	}
	return nil
}

func (r *PostgresRepository) CountPortfolios(ctx context.Context, userID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM stackvest.portfolios WHERE user_id = $1`,
		userID,
	).Scan(&count)
	return count, err
}

// --- Positions ---

func (r *PostgresRepository) Add(ctx context.Context, portfolioID, symbol, name string, shares, avgCost float64) (*portfoliodomain.Position, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var pos portfoliodomain.Position
	err = tx.QueryRow(ctx,
		`INSERT INTO stackvest.portfolio_positions (portfolio_id, symbol, name, shares, avg_cost)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, portfolio_id, symbol, name, shares, avg_cost, added_at`,
		portfolioID, symbol, name, shares, avgCost,
	).Scan(&pos.ID, &pos.PortfolioID, &pos.Symbol, &pos.Name, &pos.Shares, &pos.AvgCost, &pos.AddedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, portfoliodomain.ErrAlreadyExists
	}
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO stackvest.portfolio_activity (portfolio_id, symbol, label, detail, tone, badge)
		 VALUES ($1, $2, $3, $4, 'positive', 'BUY')`,
		portfolioID, symbol,
		fmt.Sprintf("Bought %s", symbol),
		fmt.Sprintf("%g shares @ $%.2f", shares, avgCost),
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &pos, nil
}

func (r *PostgresRepository) Remove(ctx context.Context, portfolioID, symbol string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Activity inserted before deletion; if deletion finds 0 rows, tx is rolled back
	// via defer so the activity row is never persisted.
	_, err = tx.Exec(ctx,
		`INSERT INTO stackvest.portfolio_activity (portfolio_id, symbol, label, detail, tone, badge)
		 VALUES ($1, $2, $3, 'Position closed', 'neutral', 'SELL')`,
		portfolioID, symbol,
		fmt.Sprintf("Sold %s", symbol),
	)
	if err != nil {
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

	return tx.Commit(ctx)
}

func (r *PostgresRepository) Update(ctx context.Context, portfolioID, symbol string, shares, avgCost *float64) (*portfoliodomain.Position, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var pos portfoliodomain.Position
	err = tx.QueryRow(ctx,
		`UPDATE stackvest.portfolio_positions
		 SET shares   = COALESCE($3, shares),
		     avg_cost = COALESCE($4, avg_cost)
		 WHERE portfolio_id = $1 AND symbol = $2
		 RETURNING id, portfolio_id, symbol, name, shares, avg_cost, added_at`,
		portfolioID, symbol, shares, avgCost,
	).Scan(&pos.ID, &pos.PortfolioID, &pos.Symbol, &pos.Name, &pos.Shares, &pos.AvgCost, &pos.AddedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, portfoliodomain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO stackvest.portfolio_activity (portfolio_id, symbol, label, detail, tone, badge)
		 VALUES ($1, $2, $3, $4, 'neutral', 'UPDATE')`,
		portfolioID, symbol,
		fmt.Sprintf("Updated %s", symbol),
		fmt.Sprintf("%g shares @ $%.2f", pos.Shares, pos.AvgCost),
	)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &pos, nil
}

func (r *PostgresRepository) ListByPortfolioID(ctx context.Context, portfolioID string) ([]*portfoliodomain.Position, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, portfolio_id, symbol, name, shares, avg_cost, added_at
		 FROM stackvest.portfolio_positions
		 WHERE portfolio_id = $1
		 ORDER BY added_at DESC`,
		portfolioID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []*portfoliodomain.Position
	for rows.Next() {
		var pos portfoliodomain.Position
		if err := rows.Scan(&pos.ID, &pos.PortfolioID, &pos.Symbol, &pos.Name, &pos.Shares, &pos.AvgCost, &pos.AddedAt); err != nil {
			return nil, err
		}
		positions = append(positions, &pos)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if positions == nil {
		positions = []*portfoliodomain.Position{}
	}
	return positions, nil
}

func (r *PostgresRepository) CountPositions(ctx context.Context, portfolioID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM stackvest.portfolio_positions WHERE portfolio_id = $1`,
		portfolioID,
	).Scan(&count)
	return count, err
}

func (r *PostgresRepository) GetActivity(ctx context.Context, portfolioID string, limit int) ([]*portfoliodomain.Activity, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, COALESCE(symbol, ''), label, detail, tone, badge, occurred_at
		 FROM stackvest.portfolio_activity
		 WHERE portfolio_id = $1
		 ORDER BY occurred_at DESC
		 LIMIT $2`,
		portfolioID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var activities []*portfoliodomain.Activity
	for rows.Next() {
		var act portfoliodomain.Activity
		if err := rows.Scan(&act.ID, &act.Symbol, &act.Label, &act.Detail, &act.Tone, &act.Badge, &act.Timestamp); err != nil {
			return nil, err
		}
		activities = append(activities, &act)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if activities == nil {
		activities = []*portfoliodomain.Activity{}
	}
	return activities, nil
}

var _ portfoliodomain.Repository = (*PostgresRepository)(nil)
