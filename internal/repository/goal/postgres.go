package goal

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	goaldomain "github.com/kanitin/stackvest/backend/internal/domain/goal"
)

const (
	// pgInvalidTextRepresentation is raised when a path id is not a UUID.
	pgInvalidTextRepresentation = "22P02"
	// pgForeignKeyViolation is raised when portfolio_id names no portfolio, e.g. one
	// deleted between the use case's ownership check and the write.
	pgForeignKeyViolation = "23503"
)

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// isMalformedID reports whether err is Postgres refusing an id that is not a UUID. Such an
// id cannot match any row, so it is treated as "not found".
func isMalformedID(err error) bool { return pgCode(err) == pgInvalidTextRepresentation }

// mapWriteErr turns a malformed portfolio id or a dangling portfolio_id into ErrInvalidPortfolio.
func mapWriteErr(err error) error {
	switch pgCode(err) {
	case pgForeignKeyViolation, pgInvalidTextRepresentation:
		return goaldomain.ErrInvalidPortfolio
	}
	return err
}

const goalColumns = `id, user_id, portfolio_id, name, kind, currency, starting_amount, monthly_amount,
	target_amount, years, expected_return, contribution_growth, inflation, created_at, updated_at`

func scanGoal(row pgx.Row) (*goaldomain.Goal, error) {
	var g goaldomain.Goal
	err := row.Scan(
		&g.ID, &g.UserID, &g.PortfolioID, &g.Name, &g.Kind, &g.Currency, &g.StartingAmount, &g.MonthlyAmount,
		&g.TargetAmount, &g.Years, &g.ExpectedReturn, &g.ContributionGrowth, &g.Inflation, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Create(ctx context.Context, g *goaldomain.Goal, maxPerUser int) (*goaldomain.Goal, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Lock the user row so concurrent creates for the same user cannot both pass the count check.
	if _, err := tx.Exec(ctx, `SELECT id FROM stackvest.users WHERE id = $1 FOR UPDATE`, g.UserID); err != nil {
		return nil, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM stackvest.goals WHERE user_id = $1`, g.UserID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= maxPerUser {
		return nil, goaldomain.ErrGoalLimitReached
	}

	created, err := scanGoal(tx.QueryRow(ctx,
		`INSERT INTO stackvest.goals (user_id, portfolio_id, name, kind, currency, starting_amount, monthly_amount,
		     target_amount, years, expected_return, contribution_growth, inflation)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING `+goalColumns,
		g.UserID, g.PortfolioID, g.Name, g.Kind, g.Currency, g.StartingAmount, g.MonthlyAmount,
		g.TargetAmount, g.Years, g.ExpectedReturn, g.ContributionGrowth, g.Inflation,
	))
	if err != nil {
		return nil, mapWriteErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

func (r *PostgresRepository) ListByUser(ctx context.Context, userID string, limit, offset int) ([]*goaldomain.Goal, int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+goalColumns+`, COUNT(*) OVER() AS total
		 FROM stackvest.goals
		 WHERE user_id = $1
		 ORDER BY created_at DESC, id
		 LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	goals := []*goaldomain.Goal{}
	total := 0
	for rows.Next() {
		var g goaldomain.Goal
		if err := rows.Scan(
			&g.ID, &g.UserID, &g.PortfolioID, &g.Name, &g.Kind, &g.Currency, &g.StartingAmount, &g.MonthlyAmount,
			&g.TargetAmount, &g.Years, &g.ExpectedReturn, &g.ContributionGrowth, &g.Inflation, &g.CreatedAt,
			&g.UpdatedAt, &total,
		); err != nil {
			return nil, 0, err
		}
		goals = append(goals, &g)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(goals) == 0 && offset > 0 {
		// A page past the end has no rows to carry the window count.
		if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM stackvest.goals WHERE user_id = $1`, userID).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return goals, total, nil
}

func (r *PostgresRepository) Get(ctx context.Context, id string) (*goaldomain.Goal, error) {
	g, err := scanGoal(r.pool.QueryRow(ctx, `SELECT `+goalColumns+` FROM stackvest.goals WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) || isMalformedID(err) {
		return nil, goaldomain.ErrGoalNotFound
	}
	return g, err
}

func (r *PostgresRepository) Update(ctx context.Context, g *goaldomain.Goal) (*goaldomain.Goal, error) {
	updated, err := scanGoal(r.pool.QueryRow(ctx,
		`UPDATE stackvest.goals
		 SET portfolio_id = $3, name = $4, kind = $5, currency = $6, starting_amount = $7, monthly_amount = $8,
		     target_amount = $9, years = $10, expected_return = $11, contribution_growth = $12, inflation = $13,
		     updated_at = NOW()
		 WHERE id = $1 AND user_id = $2
		 RETURNING `+goalColumns,
		g.ID, g.UserID, g.PortfolioID, g.Name, g.Kind, g.Currency, g.StartingAmount, g.MonthlyAmount,
		g.TargetAmount, g.Years, g.ExpectedReturn, g.ContributionGrowth, g.Inflation,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, goaldomain.ErrGoalNotFound
	}
	if err != nil {
		return nil, mapWriteErr(err)
	}
	return updated, nil
}

func (r *PostgresRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM stackvest.goals WHERE id = $1`, id)
	if isMalformedID(err) {
		return goaldomain.ErrGoalNotFound
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return goaldomain.ErrGoalNotFound
	}
	return nil
}
