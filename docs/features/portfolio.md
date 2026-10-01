# Portfolios & positions

## Purpose

Users own multiple named portfolios. Each portfolio holds positions (symbol, shares, average cost). The feature also
serves dashboard aggregates (value, 30-day change, diversification), an activity feed, and a recorded history of the
user's total value. For the AI analysis endpoints on the same route group, see
[portfolio-analysis.md](./portfolio-analysis.md).

## Endpoints

All endpoints are protected and live under `/api/v1/portfolios`.

| Method | Path                      | Body / params                                   | Response                                                         |
|--------|---------------------------|-------------------------------------------------|------------------------------------------------------------------|
| POST   | `/`                       | `{ name*, description }`                        | `201` · `409` portfolio limit reached                           |
| GET    | `/`                       | —                                               | portfolios enriched with `value`, `assetCount`                   |
| GET    | `/summary`                | —                                               | `{ totalValue, changePct, diversificationScore }` across all portfolios |
| GET    | `/history`                | `range` ∈ `7D, 30D, 90D, 1Y, All` (default `30D`) | `{ range, points: [{ date, value }] }`, oldest first · `400` bad range |
| GET    | `/positions`              | —                                               | every position across all portfolios, enriched with live value   |
| GET    | `/activity`               | `limit` 1–50 (default 10)                       | newest activity across all portfolios, each row with `portfolioId` and `portfolioName` · `400` bad limit |
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
  `UserHolding`, `ValuePoint`, `ValueHistory`, `HistoryRange`,
  errors `ErrPortfolioNotFound`, `ErrPortfolioLimitReached`, `ErrPositionLimitReached`, `ErrAlreadyExists`,
  `ErrNotFound`, `ErrPortfolioEmpty`, `ErrPricingUnavailable`)
- Use case: `internal/usecase/portfolio/portfolio.go`, and `snapshot.go` for the value snapshot and its history
- Repository: `internal/repository/portfolio/postgres.go`
- Handler: `internal/delivery/http/handler/portfolio.go`
- Background job: `pkg/worker/periodic.go`, started in `main.go`

## Data & dependencies

- Tables `portfolios`, `portfolio_positions`, `portfolio_activity`, `user_value_snapshots`. Migrations `000004`,
  `000005`, `000007`–`000009`, `000011`. `000008` and `000009` added `portfolio_id` and backfilled a per-user
  "Default" portfolio. Positions are `UNIQUE (portfolio_id, symbol)`, so one symbol can appear in several portfolios.
- `user_value_snapshots` (`000011`) has one row per `(user_id, snapshot_date)`: the user's total USD value across all
  portfolios, as last recorded on that UTC day. It cascades on user delete only.
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
- **Route order:** static `/summary`, `/history`, `/positions`, `/activity` and `/analyze` are registered before `/:id`.
- **Cross-portfolio reads exist for the rate limit:** protected routes allow a burst of 20 requests per user
  (`RateLimit(1, 20)` in `router.go`). A dashboard that asked each of up to 10 portfolios for its positions and its
  activity would exceed that on one page load, so `/positions` and `/activity` return everything in one request each.
  `/positions` returns one row per portfolio holding, so a symbol held in two portfolios appears twice.
- **`changePct` is a back-test, `/history` is a record:** the 30-day change on `/summary` applies today's share
  counts to prices 30 days ago. `/history` is what the account was actually worth on each day. The two can disagree
  whenever positions changed inside the window.
- **Derived fields:** `value` and `assetCount` are pointers and are only filled by List/Get. Create/Update return them
  as `null`. `value` is also `null` when holdings exist but none could be priced, so the UI shows "—" instead of $0.
- **Pricing:** `fetchPrices` dedupes symbols across portfolios. It runs quote and 30-day change in parallel per
  symbol, with at most `fetchConcurrency = 10` symbols at once. Lookups are best-effort: an unpriced symbol is left
  out of the value math.
- **Diversification score:** HHI over per-symbol value weights, `round((1 − Σ wᵢ²) × 100)`. One holding scores 0.
  To change the formula, edit `diversificationScore`.

## Value snapshots

`GET /history` reads recorded values; it never prices anything. The values come from a background job.

- **Job:** `main.go` starts `worker.StartPeriodic` with a three-hour interval (`valueSnapshotInterval`). It runs once
  at startup and then every three hours, calling `SnapshotValues` with the current UTC time. Its `Stop` is registered
  in `runUntilShutdown` **before** `pool.Close()`, because the job writes through the pool.
- **FMP cost:** each run makes one quote call per distinct symbol held by any user, whether or not anyone opens the
  app. The 30-second quote cache does not help across runs. Lengthen `valueSnapshotInterval` if the FMP quota is
  tight; keep it at or under three hours so a run always lands after the US close.
- **A day settles on its last value:** each run upserts the row for the current UTC date, so the stored value is the
  last one recorded that day. The last run of a UTC day falls after 21:00 UTC and US markets close at 20:00 or 21:00
  UTC, so this is normally the closing value. Weekends and holidays record the previous close again.
- **All-or-nothing per user:** a user is written only when every holding is priced. If any quote fails, or returns a
  non-positive price, that user gets no row for the run. A partial total would chart as a dip that never happened.
  After a transient failure a later run picks the user up. A holding that can never be quoted (a delisted ticker,
  for example) blocks that user's history until the holding is removed; the job logs the count as `usersSkipped`.
- **Quote-only pricing:** `fetchQuotePrices` does not use the 30-day price change, so a failure of that lookup (which
  makes `fetchPrices` drop the symbol) costs no history.
- **Keyed by user, not portfolio:** deleting a portfolio does not change totals already recorded. The cost is that
  there is no per-portfolio history.
- **No backfill:** history starts on the day the job first runs. `portfolio_activity` rows are free text and cannot
  reconstruct earlier values.
- **Users with no positions get no row.** After a user sells everything, their series stops at the last day they
  held something; it does not drop to zero.
- **Running more than one instance is safe:** the upsert is idempotent and the instances write the same value.

## Tests

`handler/portfolio_test.go`, `usecase/portfolio/portfolio_test.go`, `usecase/portfolio/snapshot_test.go`,
`pkg/worker/periodic_test.go`
