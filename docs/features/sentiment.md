# Market sentiment

## Purpose

A daily composite market "fear/greed" style score computed from FMP signals.

## Endpoints

| Method | Path                 | Auth      | Response                                                                                   |
|--------|----------------------|-----------|--------------------------------------------------------------------------------------------|
| GET    | `/api/v1/sentiment`  | protected | `{ score, status, signals: { vix, indexChangePercent, gainersCount, losersCount }, timestamp }` · `503` when data is unavailable |

## Code map

- Domain: `internal/domain/sentiment/sentiment.go` (`ScoreFromVIX`, `ScoreFromMomentum`, `ScoreFromBreadth`,
  `CompositeScore`, `StatusFromScore`)
- Use case: `internal/usecase/sentiment/sentiment.go`
- Handler: `internal/delivery/http/handler/sentiment.go`

## Data & dependencies

- FMP: a `^VIX` quote, a `^GSPC` quote (momentum), and the biggest gainers and losers (breadth). The four calls run
  in parallel.

## Caching

- In-memory `cache.NewTTLWithNegative`: 6 h TTL (set in `main.go`) and 1 min negative TTL. Concurrent misses are
  coalesced by `TTL.Fill`.

## Rules & gotchas

- If any one of the four signals fails, the whole compute fails. The failure is negative-cached and the endpoint
  returns `503`, not `500`.

## Tests

`handler/sentiment_test.go`, `domain/sentiment/sentiment_test.go`, `usecase/sentiment/sentiment_test.go`
