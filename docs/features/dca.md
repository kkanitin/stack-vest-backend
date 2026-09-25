# DCA simulator

## Purpose

Backtests dollar-cost averaging a fixed amount into one symbol over a date range, using historical adjusted closes.

## Endpoints

| Method | Path                      | Auth      | Body                                                                                           |
|--------|---------------------------|-----------|------------------------------------------------------------------------------------------------|
| POST   | `/api/v1/dca/simulate`    | protected | `{ symbol*, startDate*, endDate* (YYYY-MM-DD), amount* > 0, frequency* ∈ daily, weekly, biweekly, monthly }` |

Responses: `200` `SimulationResult` (totals, total and annualized return, per-period `dataPoints`). `400` for
validation errors or a range too short. `404` for an unknown symbol. `500` for an upstream failure.

## Code map

- Domain: `internal/domain/dca/dca.go` (`Frequency`, `SimulationInput`, `SimulationResult`, `PriceFetcher`, errors)
- Use case: `internal/usecase/dca/simulator.go`
- Handler: `internal/delivery/http/handler/dca.go`
- Price source: FMP `GetHistoricalPrices` (adjusted close)

## Rules & gotchas

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
- **Annualized return** is CAGR on total capital, not IRR. The response says so in `annualizedReturnNote`.
- `simulateDCARequest` is a reference example for tag-based validation (`binding:"required,gt=0"`).

## Tests

`usecase/dca/simulator_test.go`
