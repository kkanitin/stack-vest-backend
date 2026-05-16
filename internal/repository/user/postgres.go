package user

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	userdomain "github.com/kanitin/stackvest/backend/internal/domain/user"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
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

func (r *PostgresRepository) FindByEmail(ctx context.Context, email string) (*userdomain.User, error) {
	var u userdomain.User
	var googleID *string
	err := r.pool.QueryRow(
		ctx,
		`SELECT id, google_id, email, name, picture, created_at, updated_at
		 FROM users WHERE email = $1`,
		email,
	).Scan(&u.ID, &googleID, &u.Email, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, userdomain.ErrNotFound
	}
	if googleID != nil {
		u.GoogleID = *googleID
	}
	return &u, err
}

func (r *PostgresRepository) Create(ctx context.Context, u *userdomain.User) (*userdomain.User, error) {
	now := time.Now()
	var result userdomain.User
	var googleID *string
	err := r.pool.QueryRow(
		ctx,
		`INSERT INTO users (email, name, picture, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $4)
		 RETURNING id, google_id, email, name, picture, created_at, updated_at`,
		u.Email, u.Name, u.Picture, now,
	).Scan(
		&result.ID, &googleID, &result.Email, &result.Name, &result.Picture,
		&result.CreatedAt, &result.UpdatedAt,
	)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return nil, userdomain.ErrAlreadyExists
	}
	if googleID != nil {
		result.GoogleID = *googleID
	}
	return &result, err
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
