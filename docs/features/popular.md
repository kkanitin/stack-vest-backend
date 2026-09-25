# Popular assets

## Purpose

A public list of popular assets for discovery screens. Crypto comes from a static curated list. Stocks come from FMP
"most active".

## Endpoints

| Method | Path              | Auth   | Params                                                                  |
|--------|-------------------|--------|-------------------------------------------------------------------------|
| GET    | `/api/v1/popular` | public | `type` ∈ `crypto` (default), `stock`, `all`; `limit` 1–50 (optional)    |

The response is a list envelope. `meta` has only `total` and `currentPageCount`: this endpoint is not paginated.

## Code map

- Handler only: `internal/delivery/http/handler/popular.go`. It holds the static `popularAssets` list and calls the
  FMP client (`GetMostActiveStocks`) directly.

## Caching

- The most-active stock list is cached with `cache.NewTTLWithNegative` (5 min TTL, 30 s negative TTL). Concurrent
  misses are coalesced by `TTL.Fill`.

## Rules & gotchas

- **FMP failures degrade and never return 500:** `type=stock` returns an empty list, and `type=all` returns crypto only.
- `type=all` with `limit` splits 60/40: `ceil(limit × 0.6)` crypto, and stocks fill the rest.

## Tests

`handler/popular_test.go`
