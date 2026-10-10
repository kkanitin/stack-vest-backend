# Market heatmap

## Purpose

A market-cap treemap of a stock index, like the finviz or TradingView S&P 500 map. Every constituent of the index
is grouped by sector and carries its market cap (the tile size) and its 1D / 1W / 1M / YTD change (the tile colour).

## Endpoints

| Method | Path                     | Auth      | Params                                       |
|--------|--------------------------|-----------|----------------------------------------------|
| GET    | `/api/v1/market/heatmap` | protected | `index` ∈ `sp500`, `nasdaq100`, `dow30` (required) |

Response (`result`):

```json
{
  "index": "sp500",
  "updatedAt": "2026-10-10T14:30:00Z",
  "sectors": [
    {
      "name": "Technology",
      "marketCap": 1.9e13,
      "stocks": [
        {
          "symbol": "MSFT", "name": "Microsoft Corporation", "subSector": "Software - Infrastructure",
          "marketCap": 3.1e12, "price": 430.1,
          "change": { "1D": 0.8, "1W": 2.1, "1M": 4.0, "YTD": 39.9 }
        }
      ]
    }
  ]
}
```

- Sectors are sorted by total market cap, descending, and so are the stocks inside each sector.
- A `change` value is `null` when that period is unavailable for the symbol. `1D` is always set.
- Errors:
  - `400` when `index` is missing or unknown.
  - `503` with `Retry-After: 30` until the first snapshot of that index is built. A cold start takes a few minutes
    for the S&P 500; Dow 30 and Nasdaq 100 are ready first.
  - `500` for anything else.

## Code map

- Domain: `internal/domain/market/heatmap.go` holds the entities and the `ConstituentLister` / `BatchQuoter`
  interfaces.
- Use case: `internal/usecase/market/heatmap.go`. It has `Refresh(ctx)` to rebuild all indexes and `Get(index)` to
  read a snapshot.
- Infrastructure:
  - `internal/infrastructure/fmp/market.go` has `ListConstituents` and `GetBatchQuotes`.
  - `internal/infrastructure/cached/constituents.go` caches the constituents.
  - `internal/infrastructure/throttled/price_changer.go` rate-limits price-change lookups.
- Handler: `internal/delivery/http/handler/market.go`
- Job: started in `main.go` with `worker.StartPeriodic`, and stopped in `runUntilShutdown`.

## Data & dependencies

- FMP:
  - `/sp500-constituent`, `/nasdaq-constituent` and `/dowjones-constituent` give the members, sector and sub-sector.
  - `/batch-quote` gives price, 1D change and market cap, in chunks of 100 symbols.
  - `/stock-price-change` gives 1W (`5D`), 1M and YTD, one call per symbol.
- Config: `market.heatmap.refresh_minutes` (default 5), `market.heatmap.change_ttl_minutes` (default 30) and
  `market.heatmap.change_calls_per_minute` (default 150). See [configuration.md](../configuration.md).

## Caching

- Snapshots live in memory, one per index, and are replaced as each index finishes a rebuild. A failed rebuild keeps
  the previous snapshot.
- Constituents: `cached.ConstituentLister`, 24 h per index.
- 1W/1M/YTD changes: a dedicated `cached.PriceChanger` with the `change_ttl_minutes` TTL. It is separate from the
  30 s cache that interactive endpoints use, and it is shared across the three indexes, so an overlapping symbol
  such as AAPL is fetched once.

## Rules & gotchas

- **FMP budget:** a rebuild needs about 520 price-change calls when the change cache is cold. Cache misses go
  through `throttled.PriceChanger`, a token bucket at `change_calls_per_minute`, so the refresh never uses up the
  per-minute plan limit (Starter: 300/min) that interactive requests share.
- **Plan fallback:** if `/batch-quote` answers 402 or 403 (not on the plan), the client remembers that and quotes one
  symbol at a time through `/quote`, at most 8 at once.
- Constituents with no quote or a zero market cap are left out, because a tile needs a size.
- A failed price-change lookup leaves only `1D` set for that symbol. It does not fail the map.
- Constituents without a sector are grouped under `Other`.

## Tests

`fmp/market_test.go`, `usecase/market/heatmap_test.go`, `handler/market_test.go`
