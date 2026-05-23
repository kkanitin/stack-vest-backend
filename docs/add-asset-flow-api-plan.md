# Add Asset Flow — Backend API Plan

> **Status:** Draft
> **Created:** 2026-05-23
> **Author:** kanitin.kr
> **Frontend TODO:** `frontend/docs/todos/add-asset-flow.md` — task A-1

## Overview

The Add Asset modal (`AddAssetModal.tsx`) shows a "Suggested" list when the
search input is empty. The list is currently a hard-coded `SUGGESTED` constant
(five assets: BTC, ETH, AAPL, NVDA, SPY — mixed crypto and stocks). Task A-1
asks for that list to come from the backend so it can be updated without a
frontend deploy.

The frontend TODO itself suggests sharing the existing `GET /api/v1/popular`
endpoint rather than adding `/suggestions`. However, `/popular` today is
**crypto-only and hard-coded (20 items)**; the `SUGGESTED` constant also covers
stocks (AAPL, NVDA, SPY). This plan extends `/popular` to be the single source
of truth for both use cases.

## Goals

- Extend `GET /api/v1/popular` to accept `?type=stock|crypto|all` and
  `?limit=N` query parameters.
- Default behaviour (`?type=crypto`) is backward-compatible — the existing
  Heatmap H-2 consumer (`usePopularAssets`) continues to work unchanged.
- The Add Asset modal fetches `?type=all&limit=10` (or `?type=stock` + merge
  client-side) so it can surface both asset classes.
- Source popular stocks from FMP `GET /stable/most-actives` (live,
  market-activity-ranked). Use the existing curated crypto seed for crypto.
- Cache FMP responses in-process for 5 minutes to respect rate limits and
  avoid per-request latency.

## Non-Goals

- A `/suggestions` endpoint or admin UI for managing the curated list.
- Personalised suggestions based on user history.
- Changes to the search flow (`GET /stocks/search`) or watchlist endpoints.
- Pagination (the consumer only needs a short list, `?limit=` is sufficient).

## Approach

### Extending `GET /api/v1/popular`

Add two optional query parameters:

| Param  | Type                     | Default    | Description |
|--------|--------------------------|------------|-------------|
| `type` | `stock \| crypto \| all` | `crypto`   | Filter by asset class. |
| `limit`| integer                  | all items  | Cap the returned list. Max `50`. |

**Default `type=crypto`** preserves the existing Heatmap behaviour without any
frontend changes.

**`type=stock`** returns top N active stocks from FMP.

**`type=all`** merges both lists; crypto first, then stocks. The Add Asset
modal will call `?type=all&limit=10`.

Response shape is unchanged:

```json
{
  "results": [
    { "symbol": "BTC", "name": "Bitcoin",   "type": "crypto", "category": ["Top 100", "L1s"] },
    { "symbol": "AAPL", "name": "Apple Inc.", "type": "stock",  "category": ["Most Active"] }
  ],
  "code": 200,
  "message": "Success",
  "errorMessage": null,
  "meta": { "total": 10, "currentPageCount": 10 }
}
```

### Data source

| Asset class | Source | Notes |
|-------------|--------|-------|
| Crypto      | Existing hand-curated slice in `popular.go` | 20 symbols; no FMP call. |
| Stock       | FMP `GET /stable/most-actives` | Returns real-time top-activity stocks ranked by volume. Cached 5 min. |

**FMP `most-actives` wrapping** — add `GetMostActiveStocks(n int)` to
`internal/infrastructure/fmp/client.go`. Parse the response into the same
`popularEntry` shape used by the crypto slice; set `type = "stock"` and
`category = ["Most Active"]`.

### In-process cache

Add a minimal TTL cache struct in `internal/infrastructure/fmp/` (or `pkg/cache/`):

```go
type ttlCache[T any] struct {
    mu      sync.RWMutex
    value   T
    expires time.Time
    ttl     time.Duration
}
```

`PopularHandler` holds a `*ttlCache[[]popularEntry]` for the stock slice.
On each request for stocks, check the cache; if expired (or empty), call FMP
and repopulate. Crypto slice is a constant — no cache needed.

This avoids a Redis/Memcached dependency and is sufficient for a low-traffic
BFF endpoint.

### Clean Architecture placement

`PopularHandler` is currently a thin handler with no use case or repository
layer — it serves a hard-coded constant. The stock enrichment via FMP is
lightweight enough that it can stay in the handler for now, holding the FMP
client as a dependency. If the logic grows, extract a `popularUseCase`.

Updated wiring in `main.go`:

```go
popularHandler := handler.NewPopularHandler(avClient)   // was: handler.NewPopularHandler()
```

## Tasks

| # | Task | File(s) | Status |
|---|------|---------|--------|
| 1 | Add `GetMostActiveStocks(n int) ([]popularEntry, error)` to FMP client | `internal/infrastructure/fmp/client.go` | [ ] |
| 2 | Add TTL cache utility | `pkg/cache/ttl.go` (new) | [ ] |
| 3 | Extend `PopularHandler`: accept FMP client dep, add `?type` + `?limit` params, merge stock/crypto lists with cache | `internal/delivery/http/handler/popular.go` | [ ] |
| 4 | Update `main.go` wiring to pass `avClient` to `NewPopularHandler` | `main.go` | [ ] |
| 5 | Update handler tests | `internal/delivery/http/handler/popular_test.go` | [ ] |
| 6 | Update OpenAPI spec | `postman/specs/openapi.yaml` | [ ] |

## FMP Tier Risk

`GET /stable/most-actives` is available on the **free FMP tier** (confirmed in
FMP docs as of May 2026). If the account is downgraded or the endpoint is
paywalled in a future plan change, the handler must **fall back gracefully**:
log a warning with `slog.WarnContext` and return only the crypto slice (no
500). Stocks are supplementary; crypto is the primary heatmap data source.

## Dependencies

- `avClient` (`*fmp.Client`) is already constructed in `main.go` — no new
  infra dependency.
- Frontend `src/api/popular.ts` (`getPopularAssets()`) calls `/popular` with
  no query params today — no change needed for the Heatmap consumer.
- Frontend `src/components/AddAssetModal.tsx` will call
  `/popular?type=all&limit=10` (covered in the frontend A-1 task).
- The shared hook `usePopularAssets` should be extended to accept a `type`
  option, or the modal can call `getPopularAssets({ type: 'all', limit: 10 })`
  directly (frontend decision, not blocked on this plan).

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| FMP `most-actives` paywalled / rate-limited | Low | Medium | Graceful fallback to crypto-only; log warning; no 500 |
| FMP adds latency to `/popular` (was zero-latency) | Medium | Low | 5-min TTL cache; first cold request adds ~200ms; subsequent requests are instant |
| `?type` default change breaks Heatmap | Low | High | Default remains `crypto`; unit tests assert default behaviour |
| Cache stale during market-hours spikes | Low | Low | List is for suggestions only; staleness of 5 min is acceptable |

## Open Questions

- [ ] Should `?type=all` interleave crypto and stocks, or crypto-first?
  Current plan: crypto-first (preserves visual order for Heatmap-aware users).
- [ ] How many stocks to fetch from FMP `most-actives` for the `type=all`
  response? Plan: `limit` param controls the total; default splits ~60/40
  crypto/stock if `limit` not given.
- [ ] Should `GET /popular` be rate-limited per IP for unauthenticated callers?
  Current plan: defer; the TTL cache already reduces FMP exposure.
- [ ] Cache warm-up on startup? Current plan: lazy (first request warms it).

## References

- Frontend TODO: `frontend/docs/todos/add-asset-flow.md` — A-1
- Frontend TODO coordination: `frontend/docs/todos/heatmap.md` — H-2 (done)
- Existing handler: `internal/delivery/http/handler/popular.go`
- Existing FMP client: `internal/infrastructure/fmp/client.go`
- Heatmap backend plan: `docs/heatmap-backend.md`
- Backend developer guide: `AGENTS.md`
