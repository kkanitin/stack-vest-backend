# Watchlist API Plan

## Overview

Backend support for the frontend Watchlist TODOs in
`frontend/docs/todos/watchlist.md` (W-1 prices/sparkline, W-2 per-row alerts).

**FMP-first approach:** prefer native FMP endpoints. Add new wrappers on the
existing `internal/infrastructure/fmp/client.go` only when no current method
covers the need. The existing `GetPriceChange` already satisfies the *24h
change* part of W-1 — nothing new is required for that.

This plan covers the **three remaining backend dependencies**:

| ID | Purpose | Endpoint |
|---|---|---|
| B-1 | Last price (replaces `mockMarket(...).price`) | `GET /api/v1/stocks/{symbol}/quote` |
| B-2 | 7d sparkline series (replaces `mockMarket(...).series`) | `GET /api/v1/stocks/{symbol}/history?range=7d` |
| B-3 | Persist per-symbol alerts toggle | `alertsEnabled` field on watchlist item + `PATCH /api/v1/watchlist/{symbol}/alerts` |

All endpoints are JWT-protected. Responses follow the existing
`response.OK` / `response.Err` envelope.

---

## B-1. `GET /stocks/{symbol}/quote`

### Request

```
GET /api/v1/stocks/AAPL/quote
Authorization: Bearer <jwt>
```

### Success Response

```json
{
  "result": {
    "symbol":        "AAPL",
    "price":         258.31,
    "change":        1.42,
    "changePercent": 0.55,
    "currency":      "USD",
    "timestamp":     "2026-05-23T20:00:00Z"
  },
  "code": 200,
  "message": "Success",
  "errorMessage": null
}
```

### Error Responses

| Scenario | HTTP | `errorMessage` |
|---|---|---|
| Missing `symbol` path param | 400 | `"path parameter 'symbol' is required"` |
| Symbol not found on FMP | 404 | `"symbol not found: XYZZ"` |
| FMP failure | 500 | `"failed to get stock quote"` |

### FMP endpoint

```
GET https://financialmodelingprep.com/stable/quote?symbol={SYMBOL}&apikey=KEY
```

FMP returns a JSON array; pick `[0]`. Empty array → `ErrSymbolNotFound`.

### Files

| Action | File | Change |
|---|---|---|
| Modify | `internal/domain/stock/stock.go` | Add `Quote` struct + `Quoter` interface |
| Modify | `internal/infrastructure/fmp/client.go` | Add `GetQuote(symbol string) (*stock.Quote, error)` + compile-time `var _ stock.Quoter = (*Client)(nil)` |
| Create | `internal/usecase/stock/quote.go` | `QuoteUseCase.Execute(symbol)` (mirrors `price_change.go`) |
| Modify | `internal/delivery/http/handler/stock.go` | Add `quoteUC` field, `RegisterRoutes` adds `GET /:symbol/quote`, `GetQuote` handler |
| Modify | `main.go` | Wire `quoteUC := stockuc.NewQuoteUseCase(avClient)` and pass into `handler.NewStockHandler` |

### Domain types

```go
type Quote struct {
    Symbol        string    `json:"symbol"`
    Price         float64   `json:"price"`
    Change        float64   `json:"change"`
    ChangePercent float64   `json:"changePercent"`
    Currency      string    `json:"currency"`
    Timestamp     time.Time `json:"timestamp"`
}

type Quoter interface {
    GetQuote(symbol string) (*Quote, error)
}
```

---

## B-2. `GET /stocks/{symbol}/history?range=7d`

### Request

```
GET /api/v1/stocks/AAPL/history?range=7d
Authorization: Bearer <jwt>
```

### Query parameters

| Param | Type | Rules |
|---|---|---|
| `range` | string | required; one of `7d`, `1M`, `3M`, `6M`, `1Y`, `5Y` |

The frontend currently needs `7d` only. The other tokens are accepted now so
the same endpoint can serve detail-page charts later — no API break required.

### Success Response

```json
{
  "result": {
    "symbol": "AAPL",
    "range":  "7d",
    "points": [
      { "date": "2026-05-16", "close": 254.12 },
      { "date": "2026-05-17", "close": 255.40 },
      ...
    ]
  },
  "code": 200,
  "message": "Success",
  "errorMessage": null
}
```

Use `close` (raw, not split-adjusted) — for a 7-day window split-adjustment is
not material and the sparkline is purely visual. (DCA uses `adjClose` because
multi-year ranges matter; that distinction stays put.)

### Error Responses

| Scenario | HTTP | `errorMessage` |
|---|---|---|
| Missing / invalid `range` | 400 | `"range must be one of: 7d, 1M, 3M, 6M, 1Y, 5Y"` |
| Symbol not found | 404 | `"symbol not found: XYZZ"` |
| FMP failure | 500 | `"failed to fetch history"` |

### Date math

```
to   = today (UTC, truncated)
from = to - 7  days (range=7d)
       to - 30 days (range=1M)
       to - 90 days (range=3M)
       ... etc.
```

A 7-day calendar window typically yields **5 trading days**. That is fine for
the sparkline (`useWatchlistQuotes` consumers can tolerate variable length).

### Files

| Action | File | Change |
|---|---|---|
| Modify | `internal/domain/stock/stock.go` | Add `HistoryRange` enum + `HistoryPoint` + `HistoryFetcher` interface |
| Modify | `internal/infrastructure/fmp/client.go` | Add `GetHistoryClose(symbol, from, to)` — reuses `historical-price-eod/full` endpoint but maps `close` (not `adjClose`). Implement as a thin sibling to `GetHistoricalPrices` to keep the DCA path untouched |
| Create | `internal/usecase/stock/history.go` | `HistoryUseCase.Execute(symbol, range)` — converts range → date window, calls fetcher |
| Modify | `internal/delivery/http/handler/stock.go` | Add `GET /:symbol/history`, validate `range` |
| Modify | `main.go` | Wire `historyUC := stockuc.NewHistoryUseCase(avClient)` |

### Domain types

```go
type HistoryRange string

const (
    Range7D  HistoryRange = "7d"
    Range1M  HistoryRange = "1M"
    Range3M  HistoryRange = "3M"
    Range6M  HistoryRange = "6M"
    Range1Y  HistoryRange = "1Y"
    Range5Y  HistoryRange = "5Y"
)

func (r HistoryRange) IsValid() bool { /* switch ... */ }
func (r HistoryRange) Days() int     { /* 7, 30, 90, 180, 365, 1825 */ }

type HistoryPoint struct {
    Date  string  `json:"date"`  // YYYY-MM-DD
    Close float64 `json:"close"`
}

type History struct {
    Symbol string         `json:"symbol"`
    Range  HistoryRange   `json:"range"`
    Points []HistoryPoint `json:"points"`
}

type HistoryFetcher interface {
    GetHistoryClose(symbol string, from, to time.Time) ([]HistoryPoint, error)
}
```

### Watchlist fan-out (frontend concern, noted for context)

The frontend will issue N parallel `history?range=7d` calls (one per watchlist
row) the same way `useWatchlistQuotes` already fans out `getStockPriceChange`.
No backend batching endpoint is being added yet — if request volume becomes a
concern, add `POST /api/v1/stocks/history/batch` later.

---

## B-3. Per-symbol alerts persistence

### Decision

**Extend the watchlist item with `alertsEnabled` rather than building a
separate alerts resource.** Reasons:

- The toggle is 1-to-1 with a watchlist row; there is no alert state without a
  row.
- `GET /watchlist` already returns the row — adding one field is zero extra
  round-trips for the list page.
- A single mutation endpoint (`PATCH /watchlist/{symbol}/alerts`) is enough;
  no separate `GET /watchlist/alerts` is needed.

This is the W-2 frontend doc's "Option 2 — extend the existing watchlist item
shape".

### API surface

#### Updated list / create response

`GET /api/v1/watchlist` and `POST /api/v1/watchlist` now return the new field:

```json
{
  "id":             "...",
  "userId":         "...",
  "symbol":         "AAPL",
  "name":           "Apple Inc.",
  "type":           "EQUITY",
  "addedAt":        "2026-05-23T...",
  "alertsEnabled":  false
}
```

New rows default `alertsEnabled = false`. `POST /watchlist` does **not**
accept the field — toggles are a separate concern. Keep the existing
`addItemRequest` (`symbol`, `name`, `type`) unchanged.

#### Toggle endpoint

```
PATCH /api/v1/watchlist/{symbol}/alerts
Authorization: Bearer <jwt>
Content-Type: application/json

{ "enabled": true }
```

Response on success:

```json
{
  "result": {
    "symbol":         "AAPL",
    "alertsEnabled":  true
  },
  "code": 200,
  "message": "Success",
  "errorMessage": null
}
```

Error responses:

| Scenario | HTTP | `errorMessage` |
|---|---|---|
| Missing / non-bool `enabled` | 400 | `"enabled must be a boolean"` |
| Symbol not in user's watchlist | 404 | `"symbol not in watchlist"` |
| DB failure | 500 | `"failed to update alerts"` |

`PATCH` (not `PUT`) — partial mutation of a single field on an existing
resource; idempotent toggles do not warrant a full-resource replacement.

### DB migration

Create `pkg/migrate/migrations/000003_add_watchlist_alerts.up.sql`:

```sql
ALTER TABLE stackvest.watchlists
    ADD COLUMN alerts_enabled BOOLEAN NOT NULL DEFAULT FALSE;
```

Down:

```sql
ALTER TABLE stackvest.watchlists DROP COLUMN alerts_enabled;
```

Default `FALSE` so existing rows backfill cleanly; no app-side data migration
required.

### Files

| Action | File | Change |
|---|---|---|
| Create | `pkg/migrate/migrations/000003_add_watchlist_alerts.up.sql` | `ALTER TABLE ... ADD COLUMN alerts_enabled` |
| Create | `pkg/migrate/migrations/000003_add_watchlist_alerts.down.sql` | Drop column |
| Modify | `internal/domain/watchlist/watchlist.go` | Add `AlertsEnabled bool \`json:"alertsEnabled"\`` to `Item`; add `SetAlertsEnabled(ctx, userID, symbol, enabled) error` to `Repository`; add `ErrAlertsUnchanged` sentinel only if needed (probably not) |
| Modify | `internal/repository/watchlist/postgres.go` | Include `alerts_enabled` in `Add` (RETURNING) and `ListByUserID` (SELECT); implement `SetAlertsEnabled` using `UPDATE ... WHERE user_id=$1 AND symbol=$2 RETURNING alerts_enabled` with `pgx.ErrNoRows` → `ErrNotFound` |
| Modify | `internal/usecase/watchlist/watchlist.go` | Add `SetAlerts(ctx, email, symbol, enabled) (Item, error)` — looks up user by email, calls repo |
| Modify | `internal/delivery/http/handler/watchlist.go` | Register `PATCH /:symbol/alerts`, bind `{ enabled bool }`, map `ErrNotFound` → 404 |

### Repository method

```go
func (r *PostgresRepository) SetAlertsEnabled(
    ctx context.Context, userID, symbol string, enabled bool,
) (*watchlistdomain.Item, error) {
    var item watchlistdomain.Item
    err := r.pool.QueryRow(ctx,
        `UPDATE stackvest.watchlists
            SET alerts_enabled = $3
          WHERE user_id = $1 AND symbol = $2
          RETURNING id, user_id, symbol, name, type, added_at, alerts_enabled`,
        userID, symbol, enabled,
    ).Scan(&item.ID, &item.UserID, &item.Symbol, &item.Name, &item.Type,
           &item.AddedAt, &item.AlertsEnabled)
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, watchlistdomain.ErrNotFound
    }
    if err != nil {
        return nil, err
    }
    return &item, nil
}
```

### Handler binding

```go
type setAlertsRequest struct {
    Enabled *bool `json:"enabled" binding:"required"`
}
```

Use `*bool` so `binding:"required"` distinguishes "missing" from "false".
After bind, dereference to a plain `bool` before passing to the use case.

---

## Wiring summary (`main.go`)

```go
quoteUC   := stockuc.NewQuoteUseCase(avClient)
historyUC := stockuc.NewHistoryUseCase(avClient)
stockHandler := handler.NewStockHandler(searchUC, priceChangeUC, quoteUC, historyUC)
```

`watchlistUC` and `watchlistHandler` constructor signatures stay the same —
the new method is a use-case method on the existing struct.

---

## Cross-cutting Conventions (per `AGENTS.md`)

- All JSON fields use `lowerCamelCase` — `alertsEnabled`, `changePercent`.
- Errors logged once at the handler layer with `slog.ErrorContext`.
- All responses use the `response.OK` / `response.Err` envelope; no raw
  `gin.H`.
- Each handler owns its routes via `RegisterRoutes(*gin.RouterGroup)` —
  `router.go` stays thin.

---

## Verification

After implementation, the frontend `useWatchlistQuotes` consumer can drop
`mockMarket` entirely:

1. `GET /api/v1/stocks/AAPL/quote` returns a numeric `price` that matches
   the live FMP value within a few seconds of staleness.
2. `GET /api/v1/stocks/AAPL/history?range=7d` returns ~5 ascending points
   with valid `close` values; `range=1M` returns ~20.
3. `GET /api/v1/watchlist` now includes `alertsEnabled: false` on existing
   rows after the migration runs.
4. `PATCH /api/v1/watchlist/AAPL/alerts { "enabled": true }` returns
   `alertsEnabled: true`; a second hard-reload of the frontend still shows
   the toggle on (this is the W-2 success criterion in the frontend doc).
5. `PATCH /api/v1/watchlist/ZZZZ/alerts` (not in user's watchlist) returns
   404 with `"symbol not in watchlist"`.
6. `GET /api/v1/stocks/AAPL/history?range=invalid` returns 400 with the
   expected enum error.

---

## Out of Scope (open as separate follow-ups when needed)

- **Quote caching.** Every watchlist row currently triggers a fresh FMP
  call. A short-TTL (5–15s) in-memory cache could be added if FMP rate
  limits become an issue.
- **Alert delivery.** `alertsEnabled` only persists the *preference*. The
  actual notification pipeline (price threshold engine, email/push delivery)
  is a separate feature.
- **Batch history endpoint.** Add `POST /stocks/history/batch` only if
  N-per-row fan-out shows up as a real problem.
