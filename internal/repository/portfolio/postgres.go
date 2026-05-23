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

func (r *PostgresRepository) Add(ctx context.Context, userID, symbol, name string, shares, avgCost float64) (*portfoliodomain.Position, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var pos portfoliodomain.Position
	err = tx.QueryRow(ctx,
		`INSERT INTO stackvest.portfolio_positions (user_id, symbol, name, shares, avg_cost)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, symbol, name, shares, avg_cost, added_at`,
		userID, symbol, name, shares, avgCost,
	).Scan(&pos.ID, &pos.UserID, &pos.Symbol, &pos.Name, &pos.Shares, &pos.AvgCost, &pos.AddedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, portfoliodomain.ErrAlreadyExists
	}
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO stackvest.portfolio_activity (user_id, symbol, label, detail, tone, badge)
		 VALUES ($1, $2, $3, $4, 'positive', 'BUY')`,
		userID, symbol,
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

func (r *PostgresRepository) Remove(ctx context.Context, userID, symbol string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Activity inserted before deletion; if deletion finds 0 rows, tx is rolled back
	// via defer so the activity row is never persisted.
	_, err = tx.Exec(ctx,
		`INSERT INTO stackvest.portfolio_activity (user_id, symbol, label, detail, tone, badge)
		 VALUES ($1, $2, $3, 'Position closed', 'neutral', 'SELL')`,
		userID, symbol,
		fmt.Sprintf("Sold %s", symbol),
	)
	if err != nil {
		return err
	}

	tag, err := tx.Exec(ctx,
		`DELETE FROM stackvest.portfolio_positions WHERE user_id = $1 AND symbol = $2`,
		userID, symbol,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return portfoliodomain.ErrNotFound
	}

	return tx.Commit(ctx)
}

func (r *PostgresRepository) Update(ctx context.Context, userID, symbol string, shares, avgCost *float64) (*portfoliodomain.Position, error) {
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
		 WHERE user_id = $1 AND symbol = $2
		 RETURNING id, user_id, symbol, name, shares, avg_cost, added_at`,
		userID, symbol, shares, avgCost,
	).Scan(&pos.ID, &pos.UserID, &pos.Symbol, &pos.Name, &pos.Shares, &pos.AvgCost, &pos.AddedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, portfoliodomain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO stackvest.portfolio_activity (user_id, symbol, label, detail, tone, badge)
		 VALUES ($1, $2, $3, $4, 'neutral', 'UPDATE')`,
		userID, symbol,
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

func (r *PostgresRepository) ListByUserID(ctx context.Context, userID string) ([]*portfoliodomain.Position, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, symbol, name, shares, avg_cost, added_at
		 FROM stackvest.portfolio_positions
		 WHERE user_id = $1
		 ORDER BY added_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var positions []*portfoliodomain.Position
	for rows.Next() {
		var pos portfoliodomain.Position
		if err := rows.Scan(&pos.ID, &pos.UserID, &pos.Symbol, &pos.Name, &pos.Shares, &pos.AvgCost, &pos.AddedAt); err != nil {
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

func (r *PostgresRepository) GetActivity(ctx context.Context, userID string, limit int) ([]*portfoliodomain.Activity, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, COALESCE(symbol, ''), label, detail, tone, badge, occurred_at
		 FROM stackvest.portfolio_activity
		 WHERE user_id = $1
		 ORDER BY occurred_at DESC
		 LIMIT $2`,
		userID, limit,
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
