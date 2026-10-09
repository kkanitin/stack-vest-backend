# Portfolios & positions

## Purpose

Users own multiple named portfolios. Each portfolio holds positions (symbol, shares, average cost), and every position
is derived from a buy/sell **transaction ledger** (see [Transaction ledger](#transaction-ledger)). The feature also
serves dashboard aggregates (value, 30-day time-weighted return, realised and unrealised P&L, diversification), an
activity feed read from the ledger, and a recorded history of the user's total value. For the AI analysis endpoints on the same route group, see
[portfolio-analysis.md](./portfolio-analysis.md).

## Endpoints

All endpoints are protected and live under `/api/v1/portfolios`.

| Method | Path                      | Body / params                                   | Response                                                         |
|--------|---------------------------|-------------------------------------------------|------------------------------------------------------------------|
| POST   | `/`                       | `{ name*, description }`                        | `201` · `409` portfolio limit reached                           |
| GET    | `/`                       | —                                               | portfolios enriched with `value`, `assetCount`                   |
| GET    | `/summary`                | —                                               | `{ totalValue, changePct, realisedPnl, unrealisedPnl, diversificationScore }` across all portfolios; `changePct` is the 30-day time-weighted return |
| GET    | `/history`                | `range` ∈ `7D, 30D, 90D, 1Y, All` (default `30D`), `benchmark?` | `{ range, points: [{ date, value, returnPct, benchmarkClose? }], benchmark? }`, oldest first · `400` bad range / unknown benchmark |
| GET    | `/benchmarks`             | —                                               | `[{ symbol, label }]`, the configured benchmark list, in config order |
| GET    | `/positions`              | —                                               | every position across all portfolios, enriched with live value   |
| GET    | `/activity`               | `limit` 1–50 (default 10)                       | newest ledger activity across all portfolios, each row with `portfolioId` and `portfolioName` · `400` bad limit |
| GET    | `/:id`                    | —                                               | enriched portfolio · `404`                                       |
| PATCH  | `/:id`                    | `{ name?, description? }` (at least one)        | portfolio · `404`                                                |
| DELETE | `/:id`                    | —                                               | `204` · `404`                                                    |
| POST   | `/:id/positions`          | `{ symbol*, name*, shares* > 0, avgCost* ≥ 0 }` | legacy wrapper, records a buy dated today · `201` position · `400` · `404` · `409` position limit |
| GET    | `/:id/positions`          | `includeClosed` (`true`/`false`, default false) | positions enriched with live value and P&L · `400` bad `includeClosed` · `404` |
| PATCH  | `/:id/positions/:symbol`  | any body                                        | always `410` "Holdings are now edited through their transactions." · `404` portfolio |
| DELETE | `/:id/positions/:symbol`  | —                                               | `204`, deletes the position **and all its transactions** · `404` |
| GET    | `/:id/transactions`       | `symbol?`, `page`, `size`                       | list envelope of transactions, newest first · `404`              |
| POST   | `/:id/transactions`       | `{ symbol*, name*, side*, quantity*, price*, fee?, note?, date* }` | `201 { transaction, position }` · `400` · `404` · `409` |
| PATCH  | `/:id/transactions/:txId` | any of `side, quantity, price, fee, note, date` (at least one) | `200 { transaction, position }` · `400` · `404` · `409` |
| DELETE | `/:id/transactions/:txId` | —                                               | `204` · `404` · `409` would leave a later sell uncovered         |
| GET    | `/:id/summary`            | —                                               | `{ totalValue, change30d, changePct30d, realisedPnl, unrealisedPnl }` |
| GET    | `/:id/activity`           | `limit` 1–50 (default 10)                       | ledger activity rows, newest first                               |

## Code map

- Domain: `internal/domain/portfolio/transaction.go` (`Transaction`, `LedgerRow`, `LedgerResult`, `ErrInsufficientShares`,
  `ErrTransactionNotFound`, `ErrFutureDate`, `ErrInvalidTransaction`), `ledger.go` (`Replay`, `ValidateDate`, `Round8`),
  `returns.go` (`DailyPoint`, `CumulativeTWR`, `TWR`, `Gain`)
- Domain: `internal/domain/portfolio/portfolio.go` (`Portfolio`, `Position`, `Activity`, `Summary`, `PortfoliosSummary`,
  `UserHolding`, `ValuePoint`, `ValueHistory`, `HistoryRange`, `Benchmark`, `BenchmarkInfo`, `ParseBenchmarks`,
  errors `ErrPortfolioNotFound`, `ErrPortfolioLimitReached`, `ErrPositionLimitReached`, `ErrAlreadyExists`,
  `ErrNotFound`, `ErrPortfolioEmpty`, `ErrPricingUnavailable`, `ErrUnknownBenchmark`)
- Use case: `internal/usecase/portfolio/portfolio.go`, and `snapshot.go` for the value snapshot and its history,
  `transactions.go` for the ledger use cases, `returns.go` for the daily series and the time-weighted return,
  `benchmark.go` for the benchmark overlay (`WithBenchmarks`, `Benchmarks`, `alignBenchmark`)
- Index closes: `internal/infrastructure/cached/history.go` (`cached.HistoryCloser`)
- Repository: `internal/repository/portfolio/postgres.go`
- Handler: `internal/delivery/http/handler/portfolio.go`, and `portfolio_transactions.go` for the transaction routes
- Background job: `pkg/worker/periodic.go`, started in `main.go`

## Data & dependencies

- Tables `portfolios`, `portfolio_transactions` (the ledger), `portfolio_positions` (a derived cache),
  `portfolio_activity` (no longer written or read), `user_value_snapshots`. Migrations `000004`, `000005`,
  `000007`–`000009`, `000011`, `000012`. `000008` and `000009` added `portfolio_id` and backfilled a per-user
  "Default" portfolio. Positions are `UNIQUE (portfolio_id, symbol)`, so one symbol can appear in several portfolios.
- `user_value_snapshots` (`000011`) has one row per `(user_id, snapshot_date)`: the user's total USD value across all
  portfolios, as last recorded on that UTC day. It cascades on user delete only.
- Config: `portfolio.max_per_user` (default 10) and `portfolio.max_positions_per_portfolio` (default 20). Env vars
  are `PORTFOLIO_MAX_PER_USER` and `PORTFOLIO_MAX_POSITIONS_PER_PORTFOLIO`.
- Config: `portfolio.benchmarks`, a list of `"SYMBOL=Label"` strings (default `SPY=S&P 500`, `QQQ=Nasdaq 100`,
  `VT=Total world`). Env var `PORTFOLIO_BENCHMARKS` is comma-separated. See [Benchmark overlay](#benchmark-overlay).
- Pricing uses the shared cached `Quoter` and `PriceChanger` (see [stock.md](./stock.md)). The returns use the
  cached 5-year daily closes (`cached.HistoryCloser`, the same fetcher as the benchmark overlay; see
  [Returns](#returns-time-weighted)).

## Rules & gotchas

- **IDOR guard:** every `:id` use-case method goes through `ownedPortfolio()` (email → user → load portfolio → check
  `UserID`). A missing portfolio and a portfolio owned by someone else both return `ErrPortfolioNotFound` → **404,
  never 403**.
- **One DB transaction per ledger write:** create, update and delete a transaction (and removing a position) each open
  a `pgx.Tx`, lock the portfolio row, change the ledger and rewrite the `portfolio_positions` cache in that same
  transaction. There is no separate "log activity" call: the feed is read from the ledger.
- **Partial updates:** PATCH request DTOs use pointer fields (`*string`, `*float64`), and the SQL uses `COALESCE`.
- **Numeric "0 is valid but missing is not":** `addPosition` uses `*float64` for `shares` and `avgCost` with an
  explicit `nil` check, because `binding:"required"` would reject `avgCost: 0`. The transaction request uses
  `*float64` with `required,gte=0` for `price` for the same reason.
- **Limits:** the portfolio limit is enforced in the use case (count, then compare) and returns `409`. The position
  limit is enforced in the repository, under the portfolio lock, and counts **open** positions only. It applies
  only to a write that turns a not-open symbol (new, or closed) into an open one, so adding to a held symbol or
  recording a sell never hits it.
- **Route order:** static `/summary`, `/history`, `/benchmarks`, `/positions`, `/activity` and `/analyze` are
  registered before `/:id`. A new static `GET` route must go above it too, or `/:id` swallows it as a portfolio ID.
- **Cross-portfolio reads exist for the rate limit:** protected routes allow a burst of 20 requests per user
  (`RateLimit(1, 20)` in `router.go`). A dashboard that asked each of up to 10 portfolios for its positions and its
  activity would exceed that on one page load, so `/positions` and `/activity` return everything in one request each.
  `/positions` returns one row per portfolio holding, so a symbol held in two portfolios appears twice.
- **Returns come from the ledger:** `changePct`, `changePct30d` and `change30d` are a 30-day time-weighted return
  rebuilt from the transactions, so buying or selling inside the window is neutral. `/history` `value` is still the
  recorded snapshot of what the account was worth. See [Returns](#returns-time-weighted).
- **Closed holdings (`shares = 0`)** are kept so their realised P&L stays visible. Every reader that counts or values
  holdings filters `shares > 0`: the position limit, `assetCount`, `value`, `GET /positions` and
  `GET /:id/positions` (unless `includeClosed`), the snapshot job (`ListAllHoldings`), the summaries, AI analysis and
  the dividend calendar's current shares.
- **Derived fields:** `value` and `assetCount` are pointers and are only filled by List/Get. Create/Update return them
  as `null`. `value` is also `null` when holdings exist but none could be priced, so the UI shows "—" instead of $0.
- **Pricing:** `fetchPrices` dedupes symbols across portfolios. It runs quote and 30-day change in parallel per
  symbol, with at most `fetchConcurrency = 10` symbols at once. Lookups are best-effort: an unpriced symbol is left
  out of the value math. It still backs `GET /` and `GET /:id`, whose `value` uses the old 30-day-change
  back-test helper `aggregateValue` (kept only for `enrichPortfolios`). The two summaries price by **quote only**
  (`fetchQuotePrices`), so a failed 30-day-change lookup no longer blanks them.
- **Diversification score:** HHI over per-symbol value weights, `round((1 − Σ wᵢ²) × 100)`. One holding scores 0.
  To change the formula, edit `diversificationScore`.

## Transaction ledger

`portfolio_transactions` is the source of truth. `portfolio_positions` is a **derived cache** of it, one row per
`(portfolio_id, symbol)`.

- **Table (`000012_transaction_ledger`):** `id`, `portfolio_id` (FK, cascade), `symbol`, `name`, `side`
  (`buy`/`sell`), `quantity` (> 0), `price` (≥ 0), `fee` (≥ 0, default 0), `note`, `trade_date` (DATE),
  `is_opening`, `created_at`; the numbers are `NUMERIC(20,8)`. The same migration adds
  `portfolio_positions.realised_pnl` and relaxes the shares check to `shares >= 0`.
- **Cache rewrite:** every ledger write takes the portfolio row `FOR UPDATE`, loads that symbol's transactions,
  replays them (`domain/portfolio/ledger.go`, `Replay`) and upserts the position row in the **same DB transaction**.
  With no transactions left the position row is deleted. A replay error (oversell) rolls the whole write back.
- **Closed holding:** `shares = 0` with the row kept. A later buy reopens it (`added_at` is refreshed).
- **Carry-over:** the migration inserted one opening buy per existing position (`quantity = shares`,
  `price = avg_cost`, `fee = 0`, `trade_date = (added_at AT TIME ZONE 'UTC')::date`, `is_opening = true`) with
  `created_at = added_at`,
  so shares and average cost come out unchanged and the activity feed is not flooded with "today" rows.
- **Rollback:** the down migration deletes `shares = 0` rows, drops `portfolio_transactions` and `realised_pnl`. The
  transaction history and all realised P&L are **lost**.

### Rules (`Replay`)

- **Order:** by `trade_date`, then buys before sells on the same day, then `created_at`.
- **Average cost only:** a buy adds `quantity × price + fee` to cost (fees are part of cost) and `quantity` to shares;
  average = cost / shares.
- **Sell:** leaves the average cost unchanged and realises `(price − avg) × quantity − fee`.
- **Oversell refused:** a sell above the shares held at that point is `ErrInsufficientShares` (`409`).
- **Fresh average:** selling to zero resets cost, so a buy after a full sell starts a new average.
- **Rounding:** shares, average cost and realised P&L are rounded to 8 decimal places (`Round8`).
- **No future dates:** `date` is `YYYY-MM-DD` and may not be after today in UTC (`400`).
- **Edits and deletes** reload the symbol's transactions, apply the change, replay, and refuse (`409`) if any sell
  would then be uncovered. The figures after the change are recalculated by the same replay.
- **Edits patch the locked row:** the use case reads the row only to validate the patch; the repository re-reads the
  row **inside the locked DB transaction** and applies the patch there, so a concurrent edit of other fields is merged
  rather than overwritten.
- **Reopening counts against the limit:** an edit or delete that takes a symbol from 0 shares to more than 0 (for
  example turning a sell into a buy, or deleting the sell that closed it) is checked against the open-position limit
  under the same lock, like a buy of a new symbol (`409`, everything rolled back).
- **Number bounds:** `quantity`, `price` and `fee` must be below `1e12` (the `NUMERIC(20,8)` columns hold 12 integer
  digits); anything larger is `400`, not a database error.
- **Malformed ids:** a `:txId` (or `:id`) that is not a UUID can match nothing and answers `404`, not `500`.
- **`symbol` cannot change** on an edit; delete the transaction and add a new one instead.
- **Edit validation:** a `date` that is not sent is not re-checked, so editing the fee of an old row never fails on
  its date. A sent `date` must be well formed and not in the future. Omitting `note` (or sending `null`) keeps it; a
  string, including `""`, replaces it. The API cannot set it back to NULL, so a note can be changed but not cleared.

### Transaction endpoints

- **`GET /:id/transactions`** takes `symbol` (case-insensitive, optional) and the standard `page`/`size`
  pagination (sliced in memory). Rows are newest first (`trade_date`, then `created_at`). Each row is
  `{ id, symbol, name, side, quantity, price, fee, note?, date, isOpening, runningShares, realisedPnl? }`.
  `runningShares` is on every row and `realisedPnl` on sells only, both computed per symbol over its **full**
  history, so they do not depend on the page or on the `symbol` filter.
- **`POST /:id/transactions`:** `symbol`, `name`, `side` (`buy`|`sell`), `quantity` (> 0), `price` (≥ 0) and `date`
  are required; `fee` (≥ 0, default 0) and `note` (max 500) are optional. Returns `201 { transaction, position }`. A
  buy creates the position or reopens a closed one. The symbol is trimmed and upper-cased.
- **`PATCH /:id/transactions/:txId`:** any of `side, quantity, price, fee, note, date`; an empty body is `400`.
  Returns `200 { transaction, position }`.
- **`DELETE /:id/transactions/:txId`:** `204`. Deleting the symbol's last transaction removes the position row.
- **`GET /:id/positions?includeClosed=true`** also returns closed holdings.
- **`POST /:id/positions` (legacy wrapper):** records a buy dated today (UTC) with fee 0 and returns the position, so
  clients deployed before the ledger keep working. As before the ledger, a symbol that is already an **open**
  position answers `409` "position already exists: SYM" (the check is made before the insert, so two racing requests
  can still both add a buy); a **closed** holding may be reopened this way. `409` is also returned for the position
  limit. A blank (whitespace-only) `symbol` or `name`, an overflowing number or a bad date is `400`, as on
  `POST /:id/transactions`.
- **`PATCH /:id/positions/:symbol`:** always `410 Gone`, message "Holdings are now edited through their
  transactions." The ownership check runs first, so a missing portfolio is still `404`. The route is to be removed in a
  later release.
- **`DELETE /:id/positions/:symbol`:** deletes the position and every transaction of that symbol, open or closed
  (the "entered by mistake" path); `404` if there is no position row.

**`Position`** gained `costBasis`, `unrealisedPnl`, `unrealisedPnlPct`, `realisedPnl` and `closed`.
`costBasis = shares × avgCost`. `valueUsd`, `unrealisedPnl` and `unrealisedPnlPct` are `0` for a closed holding
(which is not quoted) and when the quote is unavailable.

**Errors:**

| Status | When |
|--------|------|
| `400`  | binding failure (missing field, bad `side`, `quantity ≤ 0`, negative price/fee, `quantity`/`price`/`fee` ≥ 1e12, `note` over 500), bad `YYYY-MM-DD`, future date, bad `page`/`size`, empty PATCH body |
| `404`  | portfolio missing or not yours, transaction missing (or in another portfolio) |
| `409`  | `ErrInsufficientShares`, message "This would leave you selling 5 AAPL on 2026-03-02 when you held 3."; open-position limit reached |

### Activity

`GET /activity` and `GET /:id/activity` read `portfolio_transactions`, newest `created_at` first. Nothing writes
`portfolio_activity` any more; the table is left in place, unread. The row shape is unchanged, and the activity `id` is
the transaction id.

| Row | Label | Detail | Badge / tone |
|-----|-------|--------|--------------|
| buy | `Bought 5 AAPL` | `@ $190.00` | `BUY` / positive |
| sell | `Sold 2 AAPL` | `@ $190.00` | `SELL` / negative |
| opening entry | `Opening balance` | `5 AAPL @ $190.00` | `BUY` / positive |

Edits and deletes change or remove the underlying row, so the feed shows the current ledger, not a log of changes.

## Returns (time-weighted)

`domain/portfolio/returns.go` holds the maths and `usecase/portfolio/returns.go` builds the series.

- **Where it shows up:**
  - `GET /:id/summary`: `changePct30d` is the 30-day time-weighted return (%), and `change30d` is the dollar gain over
    the window **excluding money added or withdrawn**.
  - `GET /summary`: `changePct` is the same figure over all the user's portfolios combined.
  - Both summaries add `realisedPnl` (every symbol's replayed realised P&L, closed holdings included) and
    `unrealisedPnl` (`Σ shares × (price − avgCost)` over open positions with a live price).
  - `GET /history`: each point has `returnPct`, the cumulative TWR (%) from the first point of the range (`0` on the
    first point). It is `0` on every point when there are fewer than two points, no transactions, or no prices.
    `value` still comes from the snapshots.
- **Algorithm:**
  0. Transactions are applied in ledger order (`domain.TransactionBefore`: date, buys before sells, `created_at`), the
     same order as `Replay`, regardless of the newest-first order the repository returns them in.
  1. Build a daily UTC series from the window start to today by replaying the ledger against daily closes
     (forward-filled over weekends and holidays). Today's price is the live quote, so the headline matches the value
     shown.
  2. Each day has a value `V` and a flow `F`: a buy is `quantity × price + fee`, a sell is `−(quantity × price − fee)`.
  3. Daily return is `(V_d − F_d) / V_{d−1} − 1`; days whose previous value is 0 are skipped. The cumulative return is
     the product of `(1 + r)` minus 1, and the gain is the sum of `V_d − F_d − V_{d−1}`.
  4. An **opening entry** counts as a flow at that day's market close (not at its average cost), so a carried-over
     holding does not read as a one-day jump.
  5. A symbol with no price on a day is left out of that day's value and flow. On the first day it can be priced, its
     whole value enters as a flow, so it never reads as a gain either.
- **Window:** the summaries use today and the 30 days before it. For `/history` the series spans the first to the last
  point returned.
- **Failure behaviour:** returns never fail a request. A nil history fetcher, a failing fetch (logged as a warning) or a
  failing transaction load leaves the figures at `0`.
- **Cost:** it depends on the cached 5-year closes (`benchmarkFetcher`, i.e. `cached.HistoryCloser`, set by
  `WithBenchmarks`). Each request makes **one closes fetch per active symbol** (held at the start of the window or
  traded after it), at most 10 at once; the cache absorbs repeats. Closes older than five years are never served, so
  those days count as unpriced.
- **Not a value change:** because flows are excluded, `change30d` is not `value now − value 30 days ago`.

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
- **No backfill:** snapshot history starts on the day the job first runs; nothing writes earlier rows. The ledger
  could reconstruct earlier values, but that is not done. `returnPct` is computed from the ledger and daily closes for
  the dates that already have a snapshot point, so it never extends the chart. Ledger history itself is only as
  accurate as its inputs: a pre-ledger holding is one opening entry dated `added_at`.
- **Users with no positions get no row.** After a user sells everything, their series stops at the last day they
  held something; it does not drop to zero.
- **Running more than one instance is safe:** the upsert is idempotent and the instances write the same value.

## Benchmark overlay

`GET /history?benchmark=SPY` adds an index line to compare the portfolio against. Without `benchmark` the response is
exactly the plain history (no new fields).

- **Config list:** the allowed symbols come from `portfolio.benchmarks`, parsed once at startup by
  `portfolio.ParseBenchmarks` (trim, upper-case the symbol, split on the first `=`, label falls back to the symbol,
  bad entries skipped). `GET /benchmarks` serves that list. The `=` separator is used because `SPY: x` would parse as
  a YAML map and the env override only supports plain string lists.
- **Validation:** the handler checks `benchmark` by hand against the list, **case-sensitively**; anything else is
  `400` with `benchmark must be one of: SPY, QQQ, VT` (built from the configured list). The use case also returns
  `ErrUnknownBenchmark` as a guard.
- **Alignment:** each point gets `benchmarkClose`, the index close on its date or, on days the market was closed
  (weekends, holidays), the latest earlier close (forward fill, `alignBenchmark`: one pass over two date-sorted
  lists). The use case requests closes from 7 days before the first point so a weekend first day still has one.
  Points before the first available close have no `benchmarkClose` (the field is omitted).
- **`available` flag:** `benchmark: { symbol, label, available }`. It is `false` when the fetcher is nil or the fetch
  fails; the response stays `200`, carries no `benchmarkClose` fields, and a warning is logged. A benchmark outage
  never causes a `500`. With fewer than two points there is nothing to compare, so the provider is **not** called and
  `available` is `true` with no closes (the client shows its not-enough-history state).
- **Wiring:** `portfoliouc.New` is unchanged; `main.go` calls `WithBenchmarks(fetcher, list)` on the result.
- **Caching (`cached.HistoryCloser`, wraps the FMP `GetHistoryClose`):** keyed by symbol only. A miss always fetches
  the whole window `[today - 5y - 14d, today]` and each call clips it to its own `from`/`to` in memory, so one upstream
  call serves every range. TTL is 6 hours. Concurrent misses are coalesced into one fetch (`cache.Keyed`). Errors are
  **not** cached, so the next request retries. Closes before the five-year window are never served.

## Rollout

Deploy the **backend first, then the frontend**. `POST /:id/positions` keeps working, so older clients can still add
holdings; only the old edit-holding dialog breaks (it gets `410`) until the new frontend ships. Migration `000012`
runs at startup and creates the opening entries.

## Tests

`handler/portfolio_test.go`, `handler/portfolio_transactions_test.go`, `usecase/portfolio/portfolio_test.go`,
`usecase/portfolio/transactions_test.go`, `usecase/portfolio/returns_test.go`, `usecase/portfolio/snapshot_test.go`,
`usecase/portfolio/benchmark_test.go`, `domain/portfolio/portfolio_test.go`, `domain/portfolio/ledger_test.go`,
`domain/portfolio/returns_test.go`,
`infrastructure/cached/history_test.go`, `pkg/config/config_test.go`, `pkg/worker/periodic_test.go`
