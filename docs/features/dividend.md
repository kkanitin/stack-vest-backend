# Dividend calendar

## Purpose

Dividend payouts for the authenticated user's holdings (across all their portfolios) in a date range the caller
chooses, past or upcoming. Each entry has an estimated payout of `shares × dividend`.

## Endpoints

| Method | Path                           | Auth      | Params                                                  |
|--------|--------------------------------|-----------|---------------------------------------------------------|
| GET    | `/api/v1/dividends/calendar`   | protected | `from`, `to` (YYYY-MM-DD, optional), `page`, `size`     |

The response is a list envelope, paginated in memory and sorted by reference date, then symbol. The reference date
is the payment date, or the ex-date when the payment date is unknown.

Each entry's `shares` is the user's holding at that event's ex-date (see [Shares used](#shares-used)).

`from` and `to` are honored as sent, including past dates. Defaults: `from` is today (UTC), `to` is `from` + 75 days,
capped at the latest allowed day so a lone `from` near the upper bound is still accepted. The range is inclusive on
both ends.

`400` when:

- a date is malformed;
- `to` is before `from` (also when only `to` is sent and it is before today);
- the span `to − from` is more than 92 days (also when only `to` is sent and it is more than 92 days after today);
- `from` is earlier than the first day of (current month − 13 months), or `to` is later than the last day of
  (current month + 13 months).

## Shares used

Shares are aggregated per symbol across all of the user's portfolios.

- **Past events** (ex-date today or earlier) use the shares held at the **end of the day before the ex-date**, from the
  ledger: buys minus sells with a trade date before the ex-date, summed over all portfolios. A position opened on or
  after the ex-date earns nothing, and one sold since still counts. An event with no ex-date uses its reference date.
- **Future events** (ex-date after today) use the **current** total shares.
- **Events with 0 shares are dropped**, so they are not in the response or its `meta.total`.
- **A symbol with no ledger rows** falls back to the current shares (the case without `WithLedger`, where every event
  does). The ledger is loaded once per request.
- `estimatedAmount = shares × dividend`.

## Code map

- Domain: `internal/domain/dividend/dividend.go` (`Event`, `CalendarEntry`, `Fetcher`, `Cache`, and the range errors
  `ErrInvalidRange`, `ErrRangeTooLong`, `ErrRangeOutOfBounds`)
- Use case: `internal/usecase/dividend/calendar.go` (`CalendarUseCase`, `WithLedger`, `resolveWindow`)
- Repository: `internal/repository/dividend/redis.go` (`RedisCache`)
- Infrastructure: FMP `GetDividendsCalendar(from, to)`
- Handler: `internal/delivery/http/handler/dividend.go`

## Data & dependencies

- **Redis**, the only Redis consumer so far. Config: `redis.addr`, `redis.password`, `redis.db`.
- FMP `/stable/dividends-calendar?from=&to=`: market-wide. It filters on the **ex-date** and serves past ranges as
  well as forward-dated ones. One call returns at most 4000 rows (see [FMP row cap](#fmp-row-cap)).
  `/stable/dividends?symbol=` is history-only and **can't** be used for upcoming payouts.
- Current holdings come from `portfolio.Repository.ListPositionsByUser` (open positions only). Past events read the
  transaction ledger through `ListTransactionsByUser`, wired in `main.go` with `WithLedger(portfolioRepo)`. See
  [portfolio.md](./portfolio.md#transaction-ledger).

## Caching design

- Dividend schedules are market-wide reference data, so **the cached blobs serve every user**. There is never a
  per-user fetch.
- There is **one blob per calendar month**. Key: `dividend:v1:calendar:YYYY-MM`. The `v1` prefix lets the encoding
  change safely. A request reads the buckets of the months its range overlaps (at most five for a 92-day range), one
  after the other, and keeps the events inside the range.
- A bucket holds the events whose reference date falls in that month. To fill one, the use case fetches the ex-dates
  in `[month start − 45d, month end]` and keeps the events that belong to the month. The 45-day lookback is needed
  because FMP filters on the ex-date while the calendar places an event on its payment date, which is usually days to
  weeks later. A fill covers 45 + 31 = 76 days of ex-dates at most.
- TTL is 24 h ±10% jitter (avoids a cache avalanche). An empty month is negative-cached for 1 h. The key has no date
  stamp, so a bucket is refreshed when its TTL runs out.
- A `singleflight` in the use case, keyed per month, coalesces concurrent cold-cache fills of the same month into one.
- A fill cannot be cancelled once started (the FMP client takes no context). If the request that started it is
  abandoned, the result is still cached for the next caller, and the request stops before filling any further month.
- **Redis is non-fatal:** if Redis is down at boot or at runtime, the use case logs a warning and fetches from FMP
  directly. No other endpoint breaks, but every request then pays the full provider cost below (about 11 calls per
  month in its range), against the FMP quota the other endpoints share.

## FMP row cap

`/stable/dividends-calendar` returns **at most 4000 rows per call**. When a range holds more, it silently drops the
**earliest** ex-dates: a request for Aug 17 to Oct 31 came back as exactly 4000 rows starting at Sep 22. A single
month already exceeds the cap. A normal week is about 750–2200 rows, the busiest measured week (December 2025) was
3450, and a single peak day about 650.

The FMP client absorbs this, so callers of `GetDividendsCalendar` can pass any range:

- The range is split into contiguous 7-day tiles, fetched 4 at a time.
- A tile that comes back with 4000 rows or more is discarded and fetched again as two halves, recursively, down to a
  single day. If a single day still reaches the cap, the client logs a warning and keeps the rows it got.
- FMP can list the same symbol more than once on the same ex-date. The rows are kept as they come, never deduplicated.

Cost: each call takes about 1–6 s, scaling with its row count. Filling one cold month takes roughly 11 provider calls
or more (76 days in 7-day tiles), and the months of a request are filled one after the other, so the default 75-day
window costs three or four fills when nothing is cached. Browsing every month the API allows is about 27 buckets.
The 4-at-a-time limit is per fill, so fills of different months running at once each get their own four. When one
tile fails, tiles not yet started are skipped and the fill returns the error.

## Rules & gotchas

- The range is not clamped. A range outside the limits is a `400`, not a trimmed result. Arbitrary ranges are bounded
  to 13 months either side of the current month and to 92 days per request.
- A payout whose ex-date is more than 45 days before the start of its payment month is missed. So is one whose
  payment date falls in the month before its ex-date's month, and one the provider lists with neither date.
- An event that moves from one month to another (for example a changed payment date) can be stale for up to 24 h:
  each month's bucket is cached on its own, so the event can show in both months or in neither until both refresh.
- Amounts are an estimate, summed across currencies without conversion.
- There is no scheduler or cron: the cache fills lazily. Notifications are out of scope.

## Tests

`handler/dividend_test.go`, `usecase/dividend/calendar_test.go`, `usecase/dividend/calendar_internal_test.go`
(window validation and month helpers), `infrastructure/fmp/client_test.go` (`TestGetDividendsCalendar_*`: parsing,
tiling around the row cap, stopping after a failed tile)
