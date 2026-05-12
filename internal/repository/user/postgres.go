package user

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Migrate(ctx context.Context) error {
	_, err := r.pool.Exec(
		ctx, `
		CREATE TABLE IF NOT EXISTS users (
			id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			google_id  TEXT        UNIQUE NOT NULL,
			email      TEXT        NOT NULL,
			name       TEXT        NOT NULL,
			picture    TEXT        NOT NULL,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)
	`,
	)
	return err
}

func (r *PostgresRepository) FindByGoogleID(ctx context.Context, googleID string) (*userdomain.User, error) {
	var u userdomain.User
	err := r.pool.QueryRow(
		ctx,
		`SELECT id, google_id, email, name, picture, created_at, updated_at
		 FROM users WHERE google_id = $1`,
		googleID,
	).Scan(&u.ID, &u.GoogleID, &u.Email, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, userdomain.ErrNotFound
	}
	return &u, err
}

func (r *PostgresRepository) Upsert(ctx context.Context, u *userdomain.User) (*userdomain.User, error) {
	now := time.Now()
	var result userdomain.User
	err := r.pool.QueryRow(
		ctx,
		`INSERT INTO users (google_id, email, name, picture, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $5)
		 ON CONFLICT (google_id) DO UPDATE
		   SET email = EXCLUDED.email,
		       name = EXCLUDED.name,
		       picture = EXCLUDED.picture,
		       updated_at = EXCLUDED.updated_at
		 RETURNING id, google_id, email, name, picture, created_at, updated_at`,
		u.GoogleID, u.Email, u.Name, u.Picture, now,
	).Scan(
		&result.ID, &result.GoogleID, &result.Email, &result.Name, &result.Picture, &result.CreatedAt,
		&result.UpdatedAt,
	)
	return &result, err
}
