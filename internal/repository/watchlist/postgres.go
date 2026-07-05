package watchlist

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	watchlistdomain "github.com/kanitin/stackvest/backend/internal/domain/watchlist"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Add(ctx context.Context, item *watchlistdomain.Item) (*watchlistdomain.Item, error) {
	var result watchlistdomain.Item
	err := r.pool.QueryRow(
		ctx,
		`INSERT INTO stackvest.watchlists (user_id, symbol, name, type, category)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, symbol, name, type, added_at, alerts_enabled, category`,
		item.UserID, item.Symbol, item.Name, item.Type, item.Category,
	).Scan(&result.ID, &result.UserID, &result.Symbol, &result.Name, &result.Type, &result.AddedAt, &result.AlertsEnabled, &result.Category)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, watchlistdomain.ErrAlreadyExists
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (r *PostgresRepository) Remove(ctx context.Context, userID, symbol string) error {
	tag, err := r.pool.Exec(
		ctx,
		`DELETE FROM stackvest.watchlists WHERE user_id = $1 AND symbol = $2`,
		userID, symbol,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return watchlistdomain.ErrNotFound
	}
	return nil
}

func (r *PostgresRepository) ListByUserID(ctx context.Context, userID string) ([]watchlistdomain.Item, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id, user_id, symbol, name, type, added_at, alerts_enabled, category
		 FROM stackvest.watchlists
		 WHERE user_id = $1
		 ORDER BY added_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []watchlistdomain.Item
	for rows.Next() {
		var item watchlistdomain.Item
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.Symbol, &item.Name, &item.Type, &item.AddedAt, &item.AlertsEnabled, &item.Category,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if items == nil {
		items = []watchlistdomain.Item{}
	}
	return items, nil
}

func (r *PostgresRepository) SetAlertsEnabled(
	ctx context.Context, userID, symbol string, enabled bool,
) (*watchlistdomain.Item, error) {
	var item watchlistdomain.Item
	err := r.pool.QueryRow(ctx,
		`UPDATE stackvest.watchlists
		    SET alerts_enabled = $3
		  WHERE user_id = $1 AND symbol = $2
		  RETURNING id, user_id, symbol, name, type, added_at, alerts_enabled, category`,
		userID, symbol, enabled,
	).Scan(&item.ID, &item.UserID, &item.Symbol, &item.Name, &item.Type, &item.AddedAt, &item.AlertsEnabled, &item.Category)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, watchlistdomain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

var _ watchlistdomain.Repository = (*PostgresRepository)(nil)
