# Stocks (market data)

## Purpose

Read-only market data proxied from Financial Modeling Prep (FMP): search, quotes, price change, price history and
company profile.

## Endpoints

All endpoints are protected and live under `/api/v1/stocks`.

| Method | Path                    | Params                                                             | Response                         |
|--------|-------------------------|--------------------------------------------------------------------|----------------------------------|
| GET    | `/search`               | `keywords` (required), `page`, `size`                              | list envelope, paginated in memory |
| GET    | `/price-changes`        | `symbols` (comma-separated, max 10)                                | single object (array)            |
| GET    | `/history`              | `symbols` (max 10), `range` ∈ `7D, 30D, 90D, 1Y, All`              | single object (array)            |
| GET    | `/:symbol/price-change` | —                                                                  | single object · `404` unknown symbol |
| GET    | `/:symbol/quote`        | —                                                                  | single object · `404` unknown symbol |
| GET    | `/:symbol/history`      | `range` ∈ `7d, 1M, 3M, 6M, 1Y, 5Y`                                 | single object · `404` unknown symbol |
| GET    | `/:symbol/profile`      | —                                                                  | single object · `404` unknown symbol |

Batch endpoints return `400` when there are more than 10 symbols (`domain.ErrTooManySymbols`).

## Code map

- Domain: `internal/domain/stock/stock.go` (`Quoter`, `PriceChanger`, `Searcher`, `SymbolLister`, range types, errors)
- Use cases: `internal/usecase/stock/` (`search.go`, `quote.go`, `price_change.go`, `history.go`, `batch_price_change.go`,
  `batch_history.go`, `profile.go`)
- Infrastructure: `internal/infrastructure/fmp/client.go`, and the caching decorators in `internal/infrastructure/cached/stock.go`
- Handler: `internal/delivery/http/handler/stock.go`. The handler depends on small per-use-case interfaces so tests can stub them.

## Data & dependencies

- External: FMP `https://financialmodelingprep.com/stable`. Config: `third_party_api.fmp.api_key`.
- No DB tables.

## Caching

- **Quotes and price changes:** `cached.NewQuoter` and `cached.NewPriceChanger` (30 s TTL) are wrapped once in `main.go`.
  Every consumer shares these instances, including portfolio pricing. Use cases depend on the plain domain interface
  and don't know the cache exists.
- **Search results:** a per-normalized-keyword `cache.Keyed` (1 min TTL, max 500 entries, because callers control the keys).
  Concurrent identical misses are coalesced into one fetch.
- **Symbol universe:** the stock and ETF symbol lists are cached for 24 h (5 min negative TTL). Search uses them to keep
  only stock and ETF results.

## Rules & gotchas

- Search runs FMP symbol search and name search concurrently, merges and dedupes them (symbol matches first), then
  filters to the universe. It fails only when **both** searches fail. When the universe is unavailable or empty, it
  returns unfiltered results instead of dropping them.
- The universe fill runs without a request context, so its logs carry no request ID.

## Tests

`handler/stock_test.go`, `usecase/stock/*_test.go`, `infrastructure/fmp/client_test.go`
