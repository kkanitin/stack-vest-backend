# Financial goals

## Purpose

Users save the inputs of a financial goal (e.g. "Retirement", "House down payment") so they can come back to it later.
The backend stores **inputs only**: the projection or required-saving results are calculated in the browser.
Part of the "Financial goal simulation" epic (KAN-14); this storage API is KAN-15.

## Endpoints

All endpoints are protected and live under `/api/v1/goals`.

| Method | Path   | Body / params                       | Response                                                                 |
|--------|--------|-------------------------------------|--------------------------------------------------------------------------|
| GET    | `/`    | `page`, `size` (pagination)         | list envelope of the user's goals, newest first · `400` bad page/size    |
| POST   | `/`    | goal body (below)                   | `201` goal · `400` validation / `portfolioId not found` · `409` goal limit reached |
| GET    | `/:id` | —                                   | goal · `404`                                                             |
| PUT    | `/:id` | goal body (full replace)            | goal · `400` · `404`                                                     |
| DELETE | `/:id` | —                                   | `204` · `404`                                                            |

**Goal body** (POST and PUT take the same body; PUT replaces every field, so an omitted optional field is cleared):

| Field                | Type      | Rule                                                                       |
|----------------------|-----------|----------------------------------------------------------------------------|
| `name`               | string    | required, not blank, ≤ 100 chars; trimmed                                  |
| `kind`               | string    | required, `projection` or `target`                                         |
| `currency`           | string    | optional, ≤ 10 chars, label only; uppercased; defaults to `USD`            |
| `startingAmount`     | number    | optional, ≥ 0; defaults to 0                                               |
| `monthlyAmount`      | number    | required when `kind = projection`, ≥ 0                                     |
| `targetAmount`       | number    | required when `kind = target`, > 0                                         |
| `years`              | integer   | required, 1–100                                                            |
| `expectedReturn`     | number    | required, annual %, −100 to 100 (0 is valid)                               |
| `contributionGrowth` | number    | optional, yearly % increase of the monthly amount, −100 to 100             |
| `inflation`          | number    | optional, annual %, −100 to 100, for today's-money values                  |
| `volatility`         | number    | optional, annual %, 0 to 100 (0 is valid), for the Monte Carlo (KAN-32)    |
| `portfolioId`        | string    | optional, one of the user's portfolios; `null` or omitted means no link    |

The response goal has those fields plus `id`, `createdAt` and `updatedAt`. Optional numbers and `portfolioId` are
`null` when unset. `userId` is never returned.

## Code map

- Domain: `internal/domain/goal/goal.go` (`Goal`, `Kind`, `Repository`, `ErrGoalNotFound`, `ErrInvalidPortfolio`,
  `ErrGoalLimitReached`)
- Use case: `internal/usecase/goal/goal.go` (`ownedGoal`, `checkPortfolio`, `PortfolioGetter`)
- Repository: `internal/repository/goal/postgres.go`
- Handler: `internal/delivery/http/handler/goal.go`

## Data & dependencies

- Table `goals`, migration `000013_create_goals`; `000014_goal_volatility` adds the nullable `volatility` column
  (`CHECK 0–100`, existing goals stay `NULL`). Money columns are `NUMERIC(20,2)`, percentages `NUMERIC(9,4)`.
  `user_id` cascades on user delete. `portfolio_id` is `ON DELETE SET NULL`: deleting a portfolio keeps its linked
  goals and clears the link. Indexed on `user_id` and `portfolio_id`.
- Config: `goal.max_per_user` (default 50), env var `GOAL_MAX_PER_USER`.
- Uses the user repository (email → user ID) and the portfolio repository's `GetPortfolio` for the link check.

## Rules & gotchas

- **IDOR guard:** get, update and delete go through `ownedGoal()`. A missing goal, a malformed id and someone else's
  goal all return `404`, never `403`.
- **Linked portfolio:** checked in the use case (`checkPortfolio`). A missing or foreign portfolio returns `400`
  `portfolioId not found`, not `404`, because the goal itself is fine. A portfolio deleted between the check and the
  write hits the foreign key (`23503`), which the repository also maps to `ErrInvalidPortfolio`.
- **Kind-specific fields** use `required_if=Kind ...` binding tags. Fields that do not apply to the kind (e.g.
  `targetAmount` on a projection) are stored as sent and not cleared.
- **Numeric "0 is valid but missing is not":** `expectedReturn`, `monthlyAmount` and the other optional numbers are
  `*float64` so `0` passes `required`.
- **Limit:** enforced in the repository under a `FOR UPDATE` lock on the user row, as for portfolios, and returns `409`.

## Tests

- `internal/usecase/goal/goal_test.go`: CRUD, ownership, portfolio link, limit, currency default
- `internal/delivery/http/handler/goal_test.go`: validation per kind, status codes, response shape, pagination
- `internal/repository/goal/postgres_test.go`: Postgres error mapping
