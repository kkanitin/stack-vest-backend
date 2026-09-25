# Dividend calendar

## Purpose

Upcoming dividend payouts for the authenticated user's holdings (across all their portfolios). Each entry has an
estimated payout of `shares × dividend`.

## Endpoints

| Method | Path                           | Auth      | Params                                                  |
|--------|--------------------------------|-----------|---------------------------------------------------------|
| GET    | `/api/v1/dividends/calendar`   | protected | `from`, `to` (YYYY-MM-DD, optional), `page`, `size`     |

The response is a list envelope, paginated in memory and sorted by reference date, then symbol. The reference date
is the payment date, or the ex-date when the payment date is unknown. `400` when `to < from` or a date is malformed.

## Code map

- Domain: `internal/domain/dividend/dividend.go` (`Event`, `CalendarEntry`, `Fetcher`, `Cache`)
- Use case: `internal/usecase/dividend/calendar.go` (`CalendarUseCase`)
- Repository: `internal/repository/dividend/redis.go` (`RedisCache`)
- Infrastructure: FMP `GetDividendsCalendar(from, to)`
- Handler: `internal/delivery/http/handler/dividend.go`

## Data & dependencies

- **Redis**, the only Redis consumer so far. Config: `redis.addr`, `redis.password`, `redis.db`.
- FMP `/stable/dividends-calendar?from=&to=`: market-wide, forward-dated, 3-month maximum range.
  `/stable/dividends?symbol=` is history-only and **can't** be used for upcoming payouts.
- Holdings come from `portfolio.Repository.ListPositionsByUser`.

## Caching design

- Dividend schedules are market-wide reference data, so **one cached blob serves every user**. There is never a
  per-user fetch.
- The fetch window is fixed at `[today − 14d, today + 75d]`, about 89 days, which fits FMP's 3-month cap. The 14-day
  lookback keeps dividends that have gone ex but haven't been paid yet.
- Key: `dividend:v1:calendar:{from}:{to}`. The key is date-stamped, so it rotates daily. The `v1` prefix lets the
  encoding change safely.
- TTL is 24 h ±10% jitter (avoids a cache avalanche). An empty result is negative-cached for 1 h.
- A `singleflight` in the use case coalesces concurrent cold-cache fetches into one upstream call.
- **Redis is non-fatal:** if Redis is down at boot or at runtime, the use case logs a warning and fetches from FMP
  directly. No other endpoint is affected.

## Rules & gotchas

- The display window is clamped to `[today, today + 75d]`. A `from`/`to` outside it returns the available subset,
  not an error.
- Shares are aggregated per symbol across portfolios.
- MVP estimate limits: no ex-date eligibility check (for example, a position opened after the ex-date), and amounts
  are summed across currencies without conversion.
- There is no scheduler or cron: the cache fills lazily. Notifications are out of scope.

## Tests

`handler/dividend_test.go`, `usecase/dividend/calendar_test.go`
