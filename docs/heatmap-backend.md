# Heatmap Backend — Implementation Plan

> **Status:** Draft  
> **Created:** 2026-05-23  
> **Author:** kanitin.kr

## Overview

Two frontend heatmap tasks (H-1 and H-2) require backend changes before the mock data can be removed.
H-1 needs a `category` field on watchlist items so the filter chips (All / Top 100 / DeFi / L1s) work
against real data. H-2 needs a new `GET /popular` endpoint that returns a curated list of assets for
users whose watchlists are empty — replacing the hard-coded `MOCK_TILES` array in `HeatmapPage.tsx`.

## Goals

- Add `category` (string array) to the `watchlists` table and expose it through the existing watchlist API.
- Implement `GET /api/v1/popular` returning a list of popular assets shaped like `WatchlistEntry`.
- Keep changes additive and non-breaking — existing watchlist clients continue to work unchanged.

## Non-Goals

- Persisting user-defined categories or custom tagging.
- A full asset-catalogue service; the popular list is a lightweight curated feed, not a dynamic ranking.
- Changes to auth, portfolio, or DCA features.

## Approach

### H-1 — `category` field on watchlist items

1. **Migration**: add a `category TEXT[]` column (nullable, default `'{}'`) to `stackvest.watchlists`.
2. **Domain**: extend `WatchlistItem` struct with `Category []string`.
3. **Repository**: include `category` in `SELECT` and `INSERT`/`UPDATE` queries.
4. **Use-case / handler**: accept optional `category` in the `POST /watchlist` body; return it in `GET /watchlist` responses.
5. **OpenAPI spec**: update `WatchlistItem` schema and `POST /watchlist` request body.

The frontend's interim client-side tag map becomes the fallback until real categories are stored.
If a symbol has no stored categories the array is empty and the "All" segment still shows it.

### H-2 — `GET /api/v1/popular`

Expose a small, server-side curated list of popular crypto/stock symbols.
The endpoint is **unauthenticated** (used for the logged-out / empty-watchlist state) but can
optionally respect the JWT if present (for future personalisation).

**Response shape** — reuses `WatchlistEntry` fields so the frontend hook can feed it directly into
the heatmap tile renderer:

```json
[
  { "symbol": "BTC", "name": "Bitcoin", "type": "crypto", "category": ["Top 100", "L1s"] },
  { "symbol": "ETH", "name": "Ethereum", "type": "crypto", "category": ["Top 100", "L1s"] },
  ...
]
```

**Data source options** (pick one, ranked by preference):

| Option | Pro | Con |
|--------|-----|-----|
| Hard-coded slice in handler | Zero infra, instant | Requires redeploy to update |
| Config YAML `popular_assets` list | Easy to update without code change | Config file change still needs deploy |
| FMP `GET /v3/symbol/available-cryptocurrencies` + rank filter | Always current | Extra FMP call, rate-limit concern |

**Recommended**: hard-coded slice for now (config-backed slice as a quick follow-up).
The list is small (~20 items) and rarely changes; avoid unnecessary FMP traffic.

## Tasks

| # | Task | File(s) | Status |
|---|------|---------|--------|
| 1 | Write migration: add `category TEXT[]` to `stackvest.watchlists` | `pkg/migrate/migrations/000006_*.sql` | [ ] |
| 2 | Extend `WatchlistItem` domain struct with `Category []string` | `internal/domain/watchlist/watchlist.go` | [ ] |
| 3 | Update repository queries (SELECT, INSERT) for `category` | `internal/repository/watchlist/` | [ ] |
| 4 | Update use-case to pass `category` through add/list operations | `internal/usecase/watchlist/` | [ ] |
| 5 | Update handler: accept `category` in POST body, return in GET response | `internal/delivery/http/handler/watchlist.go` | [ ] |
| 6 | Add `PopularHandler` with curated asset list | `internal/delivery/http/handler/popular.go` (new) | [ ] |
| 7 | Register `GET /api/v1/popular` route (public, no auth middleware) | `internal/delivery/http/router/router.go` | [ ] |
| 8 | Update OpenAPI spec (`postman/specs/openapi.yaml`) | `postman/specs/openapi.yaml` | [ ] |
| 9 | Add handler-level tests for popular endpoint | `internal/delivery/http/handler/popular_test.go` (new) | [ ] |

## Dependencies

- Migration `000006` must be the next sequential migration (current latest: `000005`).
- Frontend hook `usePopularAssets()` (H-2) depends on this endpoint being deployed.
- Frontend `src/api/watchlist.ts` typings need to be updated to include `category` once Task 5 ships.

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| `category` column migration locks table on large datasets | Low | Medium | Use `ALTER TABLE … ADD COLUMN` with a default — no rewrite needed in Postgres |
| Popular list goes stale (prices/names change) | Low | Low | List only stores `symbol`+`name`+`type`; live price is fetched by the frontend via existing quote endpoints |
| FMP rate limit hit if `/popular` is called frequently unauthenticated | Low | Medium | Serve from handler constant (no FMP call); add rate-limiting middleware if needed |
| Breaking change to watchlist GET response | Low | High | `category` field is additive; existing consumers ignore unknown fields |

## Open Questions

- [ ] Should `POST /watchlist` require `category` or keep it optional (default empty)?
- [ ] Should `GET /popular` be rate-limited or cached (e.g., 5-minute in-memory cache)?
- [ ] Is ~20 symbols the right list size, or should the frontend control page size via `?limit=`?
- [ ] Do we need an admin endpoint later to manage the popular list without a redeploy?

## References

- Frontend TODO: `frontend/docs/todos/heatmap.md` — H-1 and H-2 task definitions
- Existing watchlist handler: `internal/delivery/http/handler/watchlist.go`
- Existing watchlist domain: `internal/domain/watchlist/watchlist.go`
- OpenAPI spec: `postman/specs/openapi.yaml`
- Backend developer guide: `AGENTS.md`
