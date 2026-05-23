# Portfolio API Plan

## Overview

Backend support for the frontend Dashboard Visualisation TODOs in
`frontend/docs/todos/dashboard-visualisation.md` (D-1 summary tile, D-2
allocation donut + top assets, D-3 recent activity feed).

This plan covers the three GET endpoints the dashboard needs, a thin
position-management surface (POST / DELETE / PATCH) required to seed data,
and the two new DB tables that back them.

| ID | Purpose | Endpoint |
|---|---|---|
| B-D1 | Portfolio summary (total value + 30d delta) | `GET /api/v1/portfolio/summary` |
| B-D2 | User's positions, price-enriched | `GET /api/v1/portfolio/positions` |
| B-D3 | Recent activity feed | `GET /api/v1/portfolio/activity` |
| B-D2a | Add a position (seed / UI entry) | `POST /api/v1/portfolio/positions` |
| B-D2b | Remove a position | `DELETE /api/v1/portfolio/positions/{symbol}` |
| B-D2c | Update shares / avg cost | `PATCH /api/v1/portfolio/positions/{symbol}` |

All endpoints are JWT-protected. Responses follow the existing
`response.OK` / `response.Err` envelope from `internal/delivery/http/response/`.

---

## Database Migrations

### Migration 000004 — `portfolio_positions`

`pkg/migrate/migrations/000004_create_portfolio_positions.up.sql`:

```sql
CREATE TABLE stackvest.portfolio_positions (
    id        UUID          NOT NULL DEFAULT gen_random_uuid(),
    user_id   UUID          NOT NULL REFERENCES stackvest.users(id) ON DELETE CASCADE,
    symbol    TEXT          NOT NULL,
    name      TEXT          NOT NULL,
    shares    NUMERIC(20,8) NOT NULL CHECK (shares > 0),
    avg_cost  NUMERIC(20,8) NOT NULL CHECK (avg_cost >= 0),
    added_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id),
    UNIQUE (user_id, symbol)
);

CREATE INDEX portfolio_positions_user_id_idx
    ON stackvest.portfolio_positions (user_id);
```

Down: `DROP TABLE stackvest.portfolio_positions;`

### Migration 000005 — `portfolio_activity`

`pkg/migrate/migrations/000005_create_portfolio_activity.up.sql`:

```sql
CREATE TABLE stackvest.portfolio_activity (
    id          UUID        NOT NULL DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES stackvest.users(id) ON DELETE CASCADE,
    symbol      TEXT,
    label       TEXT        NOT NULL,
    detail      TEXT        NOT NULL,
    tone        TEXT        NOT NULL CHECK (tone IN ('positive', 'negative', 'neutral')),
    badge       TEXT        NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id)
);

CREATE INDEX portfolio_activity_user_id_occurred_at_idx
    ON stackvest.portfolio_activity (user_id, occurred_at DESC);
```

Down: `DROP TABLE stackvest.portfolio_activity;`

Activity rows are inserted in the **same DB transaction** as the position
mutation — no separate logging call in the use case.

---

## Domain Types

### `internal/domain/portfolio/portfolio.go`

```go
package portfolio

import (
    "context"
    "errors"
    "time"
)

var (
    ErrNotFound     = errors.New("position not found")
    ErrAlreadyExists = errors.New("position already exists")
)

type Position struct {
    ID      string    `json:"id"`
    UserID  string    `json:"-"`
    Symbol  string    `json:"symbol"`
    Name    string    `json:"name"`
    Shares  float64   `json:"shares"`
    AvgCost float64   `json:"avgCost"`
    AddedAt time.Time `json:"addedAt"`
    // Enriched at query time — not stored in the DB.
    ValueUsd  float64 `json:"valueUsd"`
    Change24h float64 `json:"change24h"`
}

type Activity struct {
    ID        string    `json:"id"`
    Symbol    string    `json:"symbol,omitempty"`
    Label     string    `json:"label"`
    Detail    string    `json:"detail"`
    Tone      string    `json:"tone"`
    Badge     string    `json:"badge"`
    Timestamp time.Time `json:"timestamp"`
}

type Summary struct {
    TotalValue   float64 `json:"totalValue"`
    Change30d    float64 `json:"change30d"`
    ChangePct30d float64 `json:"changePct30d"`
}

type Repository interface {
    Add(ctx context.Context, userID, symbol, name string, shares, avgCost float64) (*Position, error)
    Remove(ctx context.Context, userID, symbol string) error
    Update(ctx context.Context, userID, symbol string, shares, avgCost float64) (*Position, error)
    ListByUserID(ctx context.Context, userID string) ([]*Position, error)
    LogActivity(ctx context.Context, userID string, act Activity) error
    GetActivity(ctx context.Context, userID string, limit int) ([]*Activity, error)
}
```

---

## B-D2a: `POST /portfolio/positions`

### Request

```
POST /api/v1/portfolio/positions
Authorization: Bearer <jwt>
Content-Type: application/json

{
  "symbol":  "AAPL",
  "name":    "Apple Inc.",
  "shares":  10.5,
  "avgCost": 175.00
}
```

### Success Response — 201 Created

```json
{
  "result": {
    "id":      "uuid",
    "symbol":  "AAPL",
    "name":    "Apple Inc.",
    "shares":  10.5,
    "avgCost": 175.00,
    "addedAt": "2026-05-23T..."
  },
  "code": 201,
  "message": "Success",
  "errorMessage": null
}
```

`valueUsd` / `change24h` are **not** returned on write — the client refreshes
via `GET /portfolio/positions` after mutation.

**Side effect:** inserts a `BUY` activity row in the same transaction.

### Error Responses

| Scenario | HTTP | `errorMessage` |
|---|---|---|
| Missing `symbol`, `name`, `shares`, or `avgCost` | 400 | `"symbol, name, shares and avgCost are required"` |
| `shares <= 0` | 400 | `"shares must be greater than 0"` |
| `avgCost < 0` | 400 | `"avgCost must not be negative"` |
| Symbol already in portfolio | 409 | `"position already exists: AAPL"` |
| DB failure | 500 | `"failed to add position"` |

---

## B-D2b: `DELETE /portfolio/positions/{symbol}`

Returns `204 No Content`.

**Side effect:** inserts a `SELL` activity row before deletion (same
transaction — if deletion fails the activity is not persisted).

### Error Responses

| Scenario | HTTP | `errorMessage` |
|---|---|---|
| Symbol not in user's portfolio | 404 | `"position not found: AAPL"` |
| DB failure | 500 | `"failed to remove position"` |

---

## B-D2c: `PATCH /portfolio/positions/{symbol}`

### Request

```json
{ "shares": 15.0, "avgCost": 180.00 }
```

At least one of `shares` / `avgCost` must be present; the other is left
unchanged. Same validation rules as POST.

**Side effect:** inserts an `UPDATE` activity row.

### Success Response

```json
{
  "result": {
    "id":      "uuid",
    "symbol":  "AAPL",
    "name":    "Apple Inc.",
    "shares":  15.0,
    "avgCost": 180.00,
    "addedAt": "2026-05-23T..."
  },
  "code": 200,
  "message": "Success",
  "errorMessage": null
}
```

---

## B-D2: `GET /portfolio/positions`

Returns all positions for the authenticated user, enriched with live FMP data.

### Request

```
GET /api/v1/portfolio/positions
Authorization: Bearer <jwt>
```

### Success Response

```json
{
  "result": [
    {
      "id":        "uuid",
      "symbol":    "AAPL",
      "name":      "Apple Inc.",
      "shares":    10.5,
      "avgCost":   175.00,
      "valueUsd":  2712.75,
      "change24h": 0.55,
      "addedAt":   "2026-05-23T..."
    }
  ],
  "code": 200,
  "message": "Success",
  "errorMessage": null
}
```

- `valueUsd = shares × currentPrice` (from `fmp.GetQuote`)
- `change24h` = `changePercent` from `fmp.GetQuote` (e.g. `0.55` = +0.55 %)

An empty portfolio returns `{ "result": [] }`.

### Enrichment strategy

Fan-out: call `fmp.GetQuote(symbol)` concurrently for every position using
`errgroup`. If a single quote fails, log a warning with `slog.WarnContext` and
leave `valueUsd = 0`, `change24h = 0` for that symbol rather than failing the
whole request — positions without live price are still useful for the list.

### Error Responses

| Scenario | HTTP | `errorMessage` |
|---|---|---|
| DB failure | 500 | `"failed to list positions"` |

---

## B-D1: `GET /portfolio/summary`

Aggregates the user's portfolio into the three scalars the D-1 tile needs.

### Request

```
GET /api/v1/portfolio/summary
Authorization: Bearer <jwt>
```

### Success Response

```json
{
  "result": {
    "totalValue":   24560.12,
    "change30d":    2720.00,
    "changePct30d": 12.4
  },
  "code": 200,
  "message": "Success",
  "errorMessage": null
}
```

### Computation

1. Load all positions from DB.
2. Fan-out: for each symbol, call `fmp.GetQuote` (current price) **and**
   `fmp.GetPriceChange` (1M percentage) concurrently with `errgroup`.
3. Per position:
   - `valueUsd    = shares × currentPrice`
   - `value30dAgo = shares × (currentPrice / (1 + changePct1M/100))`
4. Sums across all positions:
   - `totalValue      = Σ valueUsd`
   - `totalValue30dAgo = Σ value30dAgo`
5. `change30d    = totalValue − totalValue30dAgo`
6. `changePct30d = (change30d / totalValue30dAgo) × 100`

If a symbol's price data is unavailable, **exclude it from all sums** and log
a warning. An empty portfolio returns `{ totalValue: 0, change30d: 0,
changePct30d: 0 }`.

`fmp.GetPriceChange` already exists (`internal/infrastructure/fmp/client.go`
— used by the `/stocks/{symbol}/price-change` handler). The `1M` period key
from its response slice is what we need here.

### Error Responses

| Scenario | HTTP | `errorMessage` |
|---|---|---|
| DB failure | 500 | `"failed to load portfolio"` |

---

## B-D3: `GET /portfolio/activity`

### Request

```
GET /api/v1/portfolio/activity?limit=10
Authorization: Bearer <jwt>
```

| Param | Type | Rules |
|---|---|---|
| `limit` | int | optional; default `10`, max `50` |

### Success Response

```json
{
  "result": [
    {
      "id":        "uuid",
      "symbol":    "AAPL",
      "label":     "DCA Purchase · AAPL",
      "detail":    "10.5 shares @ $175.00",
      "tone":      "positive",
      "badge":     "BUY",
      "timestamp": "2026-05-23T10:32:00Z"
    }
  ],
  "code": 200,
  "message": "Success",
  "errorMessage": null
}
```

Activity `tone` and `badge` values are written at insertion time:

| Action | `badge` | `tone` | `label` example | `detail` example |
|---|---|---|---|---|
| Position added | `"BUY"` | `"positive"` | `"Bought AAPL"` | `"10.5 shares @ $175.00"` |
| Position removed | `"SELL"` | `"neutral"` | `"Sold AAPL"` | `"Position closed"` |
| Position updated | `"UPDATE"` | `"neutral"` | `"Updated AAPL"` | `"15 shares @ $180.00"` |

### Error Responses

| Scenario | HTTP | `errorMessage` |
|---|---|---|
| `limit < 1` or `limit > 50` | 400 | `"limit must be between 1 and 50"` |
| DB failure | 500 | `"failed to fetch activity"` |

---

## File Touchpoints

| Action | File | Change |
|---|---|---|
| Create | `pkg/migrate/migrations/000004_create_portfolio_positions.up.sql` | positions table |
| Create | `pkg/migrate/migrations/000004_create_portfolio_positions.down.sql` | drop table |
| Create | `pkg/migrate/migrations/000005_create_portfolio_activity.up.sql` | activity table |
| Create | `pkg/migrate/migrations/000005_create_portfolio_activity.down.sql` | drop table |
| Create | `internal/domain/portfolio/portfolio.go` | entities + `Repository` interface |
| Create | `internal/repository/portfolio/postgres.go` | PostgreSQL implementation |
| Create | `internal/usecase/portfolio/portfolio.go` | summary / positions / activity use cases |
| Create | `internal/delivery/http/handler/portfolio.go` | Gin handler + `RegisterRoutes` |
| Modify | `internal/delivery/http/router/router.go` | call `portfolioHandler.RegisterRoutes(v1)` |
| Modify | `main.go` | wire `portfolioRepo`, `portfolioUC`, `portfolioHandler` |

---

## Wiring (`main.go`)

```go
portfolioRepo    := portfoliorepo.NewPostgresRepository(pgPool)
portfolioUC      := portfoliouc.New(portfolioRepo, fmpClient)
portfolioHandler := handler.NewPortfolioHandler(portfolioUC)
// inside routerSetup:
portfolioHandler.RegisterRoutes(v1)
```

`fmpClient` is already wired for `GetQuote` and `GetPriceChange` (used by
existing stock handlers) — no new FMP wrapper is needed.

---

## Cross-cutting Conventions (per `AGENTS.md`)

- All JSON fields `lowerCamelCase` — `avgCost`, `valueUsd`, `change24h`, `changePct30d`.
- Errors logged once at the handler layer with `slog.ErrorContext`.
- All responses via `response.OK` / `response.Created` / `response.Err`; no raw `gin.H`.
- Each handler owns its routes via `RegisterRoutes(*gin.RouterGroup)` — `router.go` stays thin.

---

## Verification

1. `POST /api/v1/portfolio/positions` → 201; DB row inserted; activity row
   inserted with `badge: "BUY"`.
2. `GET /api/v1/portfolio/positions` → returns AAPL position with non-zero
   `valueUsd` and `change24h` matching the live FMP quote; empty portfolio
   returns `[]`.
3. `GET /api/v1/portfolio/summary` → `totalValue` equals `Σ valueUsd` from
   positions within rounding; empty portfolio returns all-zero object.
4. `GET /api/v1/portfolio/activity?limit=10` → returns the BUY entry with
   correct `badge`, `tone`, `label`, `detail`.
5. `DELETE /api/v1/portfolio/positions/AAPL` → 204; subsequent GET returns
   `[]`; activity contains SELL entry.
6. Frontend D-1 tile matches backend `totalValue`. Frontend D-2 donut
   percentages sum to 100. Frontend D-3 feed renders all returned rows without
   UI changes.

---

## Out of Scope (follow-up items)

- **Portfolio import** — bulk seed from broker CSV or brokerage API.
- **Price caching** — if N-position fan-out hits FMP rate limits, add a
  short-TTL (≤ 30 s) in-process cache.
- **30d snapshot history** — daily portfolio value snapshots for an accurate
  performance chart (vs. the current FMP 1M approximation).
- **Unrealised P&L per position** — `pnl = valueUsd − (shares × avgCost)`.
- **Alert delivery** — price-threshold engine and push/email pipeline
  (`alertsEnabled` is already stored on the watchlist; this is separate work).
