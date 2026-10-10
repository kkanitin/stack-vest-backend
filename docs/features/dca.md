# DCA simulator

## Purpose

Backtests dollar-cost averaging a fixed amount into one symbol over a date range, using historical daily closes (price movement only, no dividends).

## Endpoints

| Method | Path                      | Auth      | Body                                                                                           |
|--------|---------------------------|-----------|------------------------------------------------------------------------------------------------|
| POST   | `/api/v1/dca/simulate`    | protected | `{ symbol*, startDate*, endDate* (YYYY-MM-DD), amount* > 0, frequency* ∈ daily, weekly, biweekly, monthly }` |
| POST   | `/api/v1/dca/compare`     | protected | `{ symbols* (1 to 3), startDate*, endDate*, amount* > 0, frequency* }` — same plan for every symbol |
| POST   | `/api/v1/dca/holdings`    | protected | `{ holdings* [{ symbol*, weight* > 0 }] (1 to 20), startDate*, endDate*, amount* > 0, frequency* }` — plan split by weight |

Responses: `200` `SimulationResult` (totals, total return, two yearly returns, per-period `dataPoints`). `400` for
validation errors or a range too short. `404` for an unknown symbol. `500` for an upstream failure.

`/dca/compare` responds `200` with `{ results: SimulationResult[], skipped: [{ symbol, reason }] }`. `results` keep the request
order. `400` for the same plan errors as `/simulate`, and also for no symbols, a blank symbol or more than 3 symbols.
Symbols are upper-cased and de-duplicated. `500` only when no asset could be simulated and at least one failed for a
reason other than missing history.

`/dca/holdings` responds `200` with `{ combined, assets: [{ symbol, weightPct, result }], skipped }`. `combined` is a
`SimulationResult` with symbol `PORTFOLIO` (its `dataPoints` carry invested and value only, no price or units) and is
`null` when no holding had usable history. `400` for the same plan errors, no holdings, more than 20, a blank symbol or a
weight not above 0. The same symbol listed twice becomes one holding with the weights added. `500` only when nothing could
be simulated and a provider failure was involved.

## Code map

- Domain: `internal/domain/dca/dca.go` (`Frequency`, `SimulationInput`, `SimulationResult`, `PriceFetcher`, errors)
- Use case: `internal/usecase/dca/simulator.go`
- Handler: `internal/delivery/http/handler/dca.go`
- Price source: FMP `GetHistoricalPrices` (plain `close`), wrapped by `infrastructure/cached/dca_prices.go`

## Rules & gotchas

- **Price history is cached** per symbol and exact `startDate`/`endDate` for 6 hours (`cached.DCAPrices`, up to 500
  entries, cleared when full). Changing only the amount or frequency reuses it and makes no provider call. Concurrent
  misses share one fetch; errors are not cached, so the next request retries. Results are identical to an uncached call.
  `HistoryCloser` is not used here: it caches plain closes over a five-year window, the simulator needs a different price basis
  over up to 30 years.
- **Handler validation:**
  - Dates must be `YYYY-MM-DD`.
  - `endDate` can't be in the future, and `startDate < endDate`.
  - Maximum range per frequency: daily 5 y, weekly 15 y, biweekly 20 y, monthly 30 y (`maxDateRangeYears`).
- **Buy dates:**
  - daily: every trading day in the range.
  - weekly and biweekly: the Monday of the week containing `startDate`, then every 7 or 14 days.
  - monthly: the same day-of-month as `startDate`.
  - Each target moves to the next trading day within 7 days, and duplicates are removed. Fewer than 2 buy dates
    returns `ErrDateRangeTooShort`.
- **Price basis:** the simulator buys at the plain `close` from FMP (`historical-price-eod/full`),
  which FMP adjusts for stock splits only. `adjClose` is ignored on purpose, so **dividends are not counted as
  reinvested** and results reflect price movement only (`priceBasisNote` says so). Points with a zero close are skipped.
  This lowers results for dividend-paying assets compared with a dividend-adjusted price.
- **Closing point:** the last purchase is usually before the last trading day in the range, so when it is, `dataPoints`
  gets one extra final point on the last trading day with `unitsPurchased` 0 and `portfolioValue` equal to
  `finalPortfolioValue`. `periodsCount` counts purchases only, so it excludes this point.
- **Two yearly returns**, both in the response:
  - `annualizedReturnPct` is CAGR on total capital: it treats every purchase as made on day one, so it understates
    gradual investing (`annualizedReturnNote`).
  - `moneyWeightedReturnPct` is the annualised IRR (XIRR) of the dated purchases against the final value, so each
    purchase counts from its own date (`moneyWeightedReturnNote`). It is solved by bisection on a 365-day year and is
    `null` when no rate exists (for example a total loss).
- **Asset comparison** (`SimulatorUseCase.Compare`) runs the normal simulation for each symbol concurrently under one
  plan, and one request replaces one call per asset against the per-user request limit. An asset with no history for the
  range (`ErrSymbolNotFound`), too few buy dates (`ErrDateRangeTooShort`) or a provider failure is listed in `skipped`
  with a reason instead of failing the whole comparison. Plan validation is shared with `/simulate` (`parsePlan`).
- **Holdings simulation** (`SimulatorUseCase.SimulateHoldings`, `usecase/dca/holdings.go`) is hypothetical: each purchase
  of `amount` is split by the given weights, which the client takes from the portfolio's current values, so it does not
  replay what the user bought. One request covers the whole portfolio. At most 20 holdings, the default of
  `portfolio.max_positions_per_portfolio`, and at most 4 price histories load at once. An asset left out for missing
  history is listed in `skipped` and its weight is shared among the rest, so the full amount is still invested
  (simulation is linear in the amount, so this is a rescale of the survivors; `weightPct` is the weight after it). Per-asset
  results are scaled to their share and add up to `combined`. `combined` sums the assets' latest data point on or before each
  date, and its money-weighted return solves the IRR of all assets' dated purchases together. Today's weights favour assets
  that already did well, so the result flatters the portfolio; the frontend says so.
- **Lump-sum comparison** is not computed here: the frontend derives it from `dataPoints` (each point carries the
  price), so the endpoint needs no extra parameter.
- `simulateDCARequest` is a reference example for tag-based validation (`binding:"required,gt=0"`).

## Tests

`usecase/dca/simulator_test.go` (buy dates, return figures, closing point), `usecase/dca/compare_test.go`, `usecase/dca/holdings_test.go`,
`delivery/http/handler/dca_test.go` and `dca_holdings_test.go` (compare, holdings and shared validation), `infrastructure/cached/dca_prices_test.go`,
`infrastructure/fmp/client_test.go` (price parsing)
