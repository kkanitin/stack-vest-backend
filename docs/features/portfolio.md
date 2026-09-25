# Portfolios & positions

## Purpose

Users own multiple named portfolios. Each portfolio holds positions (symbol, shares, average cost). The feature also
serves dashboard aggregates (value, 30-day change, diversification) and an activity feed. For the AI analysis
endpoints on the same route group, see [portfolio-analysis.md](./portfolio-analysis.md).

## Endpoints

All endpoints are protected and live under `/api/v1/portfolios`.

| Method | Path                      | Body / params                                   | Response                                                         |
|--------|---------------------------|-------------------------------------------------|------------------------------------------------------------------|
| POST   | `/`                       | `{ name*, description }`                        | `201` · `409` portfolio limit reached                           |
| GET    | `/`                       | —                                               | portfolios enriched with `value`, `assetCount`                   |
| GET    | `/summary`                | —                                               | `{ totalValue, changePct, diversificationScore }` across all portfolios |
| GET    | `/:id`                    | —                                               | enriched portfolio · `404`                                       |
| PATCH  | `/:id`                    | `{ name?, description? }` (at least one)        | portfolio · `404`                                                |
| DELETE | `/:id`                    | —                                               | `204` · `404`                                                    |
| POST   | `/:id/positions`          | `{ symbol*, name*, shares* > 0, avgCost* ≥ 0 }` | `201` · `404` · `409` position limit / already exists           |
| GET    | `/:id/positions`          | —                                               | positions enriched with live value                               |
| PATCH  | `/:id/positions/:symbol`  | `{ shares?, avgCost? }` (at least one)          | position · `404` portfolio/position                              |
| DELETE | `/:id/positions/:symbol`  | —                                               | `204` · `404`                                                    |
| GET    | `/:id/summary`            | —                                               | `{ totalValue, change30d, changePct30d }`                        |
| GET    | `/:id/activity`           | `limit` 1–50 (default 10)                       | activity rows, newest first                                      |

## Code map

- Domain: `internal/domain/portfolio/portfolio.go` (`Portfolio`, `Position`, `Activity`, `Summary`, `PortfoliosSummary`,
  errors `ErrPortfolioNotFound`, `ErrPortfolioLimitReached`, `ErrPositionLimitReached`, `ErrAlreadyExists`,
  `ErrNotFound`, `ErrPortfolioEmpty`, `ErrPricingUnavailable`)
- Use case: `internal/usecase/portfolio/portfolio.go`
- Repository: `internal/repository/portfolio/postgres.go`
- Handler: `internal/delivery/http/handler/portfolio.go`

## Data & dependencies

- Tables `portfolios`, `portfolio_positions`, `portfolio_activity`. Migrations `000004`, `000005`, `000007`–`000009`.
  `000008` and `000009` added `portfolio_id` and backfilled a per-user "Default" portfolio. Positions are
  `UNIQUE (portfolio_id, symbol)`, so one symbol can appear in several portfolios.
- Config: `portfolio.max_per_user` (default 10) and `portfolio.max_positions_per_portfolio` (default 20). Env vars
  are `PORTFOLIO_MAX_PER_USER` and `PORTFOLIO_MAX_POSITIONS_PER_PORTFOLIO`.
- Pricing uses the shared cached `Quoter` and `PriceChanger` (see [stock.md](./stock.md)).

## Rules & gotchas

- **IDOR guard:** every `:id` use-case method goes through `ownedPortfolio()` (email → user → load portfolio → check
  `UserID`). A missing portfolio and a portfolio owned by someone else both return `ErrPortfolioNotFound` → **404,
  never 403**.
- **One transaction per mutation:** add, update and remove position each open a `pgx.Tx` and insert the
  `portfolio_activity` row in the same transaction. Don't add a separate "log activity" call.
- **Partial updates:** PATCH request DTOs use pointer fields (`*string`, `*float64`), and the SQL uses `COALESCE`.
- **Numeric "0 is valid but missing is not":** `addPosition` uses `*float64` for `shares` and `avgCost` with an
  explicit `nil` check, because `binding:"required"` would reject `avgCost: 0`.
- **Limits** are enforced in the use case (count, then compare) and return `409`.
- **Route order:** static `/summary` and `/analyze` are registered before `/:id`.
- **Derived fields:** `value` and `assetCount` are pointers and are only filled by List/Get. Create/Update return them
  as `null`. `value` is also `null` when holdings exist but none could be priced, so the UI shows "—" instead of $0.
- **Pricing:** `fetchPrices` dedupes symbols across portfolios. It runs quote and 30-day change in parallel per
  symbol, with at most `fetchConcurrency = 10` symbols at once. Lookups are best-effort: an unpriced symbol is left
  out of the value math.
- **Diversification score:** HHI over per-symbol value weights, `round((1 − Σ wᵢ²) × 100)`. One holding scores 0.
  To change the formula, edit `diversificationScore`.

## Tests

`handler/portfolio_test.go`, `usecase/portfolio/portfolio_test.go`
