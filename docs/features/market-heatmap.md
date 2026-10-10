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
- A `change` value is `null` when that period is unavailable for the symbol. All four are `null` together when the
  symbol's price-change lookup failed; the tile is then drawn grey.
- Errors:
  - `400` when `index` is missing or unknown.
  - `503` "heatmap is warming up" with `Retry-After: 30` while there is no snapshot yet and a build is running or
    queued. After a restart the last snapshots are loaded from Redis, so this only happens when Redis has none (first
    run, Redis down, or older than 72 h). Then the S&P 500 takes a few minutes to build; Dow 30 and Nasdaq 100 are
    ready first.
  - `503` "heatmap is temporarily unavailable" with `Retry-After: 60` when the last build of that index failed and
    there is no older snapshot to serve. The next refresh retries it.
  - `500` for anything else.

## Code map

- Domain: `internal/domain/market/heatmap.go` holds the entities and the `ConstituentLister` / `SnapshotStore`
  interfaces.
- Use case: `internal/usecase/market/heatmap.go`. It has `Refresh(ctx)` to rebuild all indexes and `Get(index)` to
  read a snapshot.
- Infrastructure:
  - `internal/infrastructure/fmp/market.go` has `ListConstituents` (FMP's live index lists, paid plans).
  - `internal/infrastructure/constituents/` holds the bundled lists (`data/*.txt`, `StaticLister`) and
    `FallbackLister`, which uses them once FMP answers that its lists are not on the plan.
  - `cmd/constituents/main.go` regenerates `data/sp500.txt`.
  - `internal/infrastructure/cached/{constituents,profile}.go` cache index members and company profiles.
  - `internal/infrastructure/throttled/throttled.go` rate-limits profile and price-change lookups on one shared limiter.
- Repository: `internal/repository/market/redis.go` (`RedisSnapshotStore`) keeps snapshots across restarts.
- Handler: `internal/delivery/http/handler/market.go`
- Job: started in `main.go` with `worker.StartPeriodic` (after `Restore` loads the stored snapshots), and stopped in
  `runUntilShutdown`.

## Data & dependencies

- **Index members:**
  - On a paid FMP plan they come from `/sp500-constituent`, `/nasdaq-constituent` and `/dowjones-constituent`.
  - On plans without them (Starter answers 402), they come from the bundled lists. Those are symbols only. Secondary
    share classes (GOOG, FOX, NWS) are left out so a company isn't drawn twice. Dotted tickers use FMP's dash form
    (`BRK-B`).
- **Per symbol:** two FMP calls, both available on Starter.
  - `/profile` gives name, sector, industry (sub-sector), market cap (tile size) and price.
  - `/stock-price-change` gives `1D`, `5D` (1W), `1M` and `ytd` (tile colour).
- **Config:** `market.heatmap.refresh_minutes` (default 5), `market.heatmap.change_ttl_minutes` (default 10) and
  `market.heatmap.calls_per_minute` (default 150). See [configuration.md](../configuration.md).

### Updating the bundled lists

- **S&P 500:** run `go run ./cmd/constituents`. It rewrites `data/sp500.txt` from the public
  `datasets/s-and-p-500-companies` CSV and refuses to write a list shorter than 450.
- **Nasdaq 100 and Dow 30:** no public machine-readable source is available, so edit `data/nasdaq100.txt` and
  `data/dow30.txt` by hand when the index changes, and bump their "Last reviewed" line.
- A symbol that has left the market fails its profile lookup. It is skipped and counted in the build's
  `missingProfiles`, so a stale list degrades gracefully.

## Caching

- Snapshots are served from memory, one per index, and are replaced as each index finishes a rebuild. A failed
  rebuild keeps the previous snapshot.
- Each built snapshot is also written to Redis under `heatmap:v1:<index>` (JSON, 72 h TTL so a weekend restart still
  has Friday's map). At startup `Restore` loads them (3 s timeout) before the first rebuild. Redis is optional: if it
  is down, the restore and the saves fail with a log line and the maps rebuild from scratch as before.
- Constituents: `cached.ConstituentLister`, 24 h per index.
- Profiles: `cached.ProfileFetcher`, 24 h per symbol. Tile size and tooltip price can therefore be up to a day old.
- Price changes: a dedicated `cached.PriceChanger` with the `change_ttl_minutes` TTL, so tile colour is at most that
  old. It is separate from the 30 s cache that interactive endpoints use.
- Both caches are shared across the three indexes, so an overlapping symbol such as AAPL is fetched once.

## Logging

The use case reports build events (`WithEvents`); `logHeatmapEvent` in `main.go` logs them. "Still waiting" and
"failing" are distinct messages and levels:

| Level | Message | Meaning |
|-------|---------|---------|
| Info  | `heatmap build started` (`index`, `symbols`) | A rebuild of one index began. |
| Info  | `FMP index lists not on plan; using bundled lists` (`index`) | Logged once per index on plans without the constituents endpoints. |
| Info  | `heatmap build in progress, waiting on rate-limited FMP calls` (`done`, `total`, `elapsed`, `etaUpTo`) | Every 30 s while profiles and price changes are being fetched. `done` rising means it is waiting, not stuck. |
| Info  | `heatmap built` (`stocks`, `elapsed`) | The index is published. |
| Warn  | `heatmap built with gaps: some stocks missing or without changes` (`missingProfiles`, `missingChanges`, `sampleError`) | Published, but `missingProfiles` stocks were left off (no profile or market cap) and `missingChanges` tiles are grey. |
| Error | `heatmap build failed` (`error`, `servingPrevious`) | The build failed. `servingPrevious=true` means users still get the older map. |
| Warn  | `heatmap snapshot not saved to Redis; a restart will rebuild it` | Persistence failed; serving is unaffected. |
| Info / Warn | `heatmap refresh finished` / `heatmap refresh finished with failures` | End of a run over all three indexes. |
| Info  | `heatmap requested while still building` (handler, with `requestId`) | A user asked before the first snapshot existed. |
| Error | `heatmap unavailable: last build failed` (handler, with `requestId`) | A user asked and there is nothing to serve because the build failed. |

## Rules & gotchas

- **FMP budget:** a cold rebuild needs about 1,040 calls (a profile and a price change for each of ~520 symbols).
  Cache misses go through `throttled` decorators that share one token bucket at `calls_per_minute`, so the refresh
  never uses up the per-minute plan limit (Starter: 300/min) that interactive requests share.
  - At the default 150/min, a first start takes about 7 minutes for the S&P 500. Dow 30 and Nasdaq 100 are ready
    first.
  - Steady state averages about 55 calls/min: price changes every `change_ttl_minutes`, profiles once a day.
- Constituents with no profile or a zero market cap are left out, because a tile needs a size.
- A failed price-change lookup leaves all four periods `null` for that symbol. It does not fail the map.
- Name, sector and sub-sector come from FMP's index list when there is one, otherwise from the profile.
  Constituents without a sector are grouped under `Other`.

## Tests

`fmp/market_test.go`, `constituents/constituents_test.go`, `throttled/throttled_test.go`,
`usecase/market/heatmap_test.go` (including restore/save with a fake store, build events, and
waiting vs failed states),
`handler/market_test.go`
