# Watchlist

## Purpose

A per-user list of tracked symbols, with an alerts toggle and free-form categories.

## Endpoints

All endpoints are protected and live under `/api/v1/watchlist`.

| Method | Path               | Body / params                                                  | Response                                                          |
|--------|--------------------|----------------------------------------------------------------|-------------------------------------------------------------------|
| GET    | `/`                | `page`, `size`                                                 | list envelope (paginated in memory)                               |
| POST   | `/`                | `{ symbol*, name*, type, category[] }`                         | `201` item · `400` invalid symbol · `409` already in watchlist   |
| DELETE | `/:symbol`         | —                                                              | `204` · `404` not in watchlist                                    |
| PATCH  | `/:symbol/alerts`  | `{ enabled* }` (`*bool`, so `false` is accepted)               | `{ symbol, alertsEnabled }` · `404`                               |

## Code map

- Domain: `internal/domain/watchlist/watchlist.go`
- Use case: `internal/usecase/watchlist/watchlist.go`
- Repository: `internal/repository/watchlist/postgres.go`
- Handler: `internal/delivery/http/handler/watchlist.go`

## Data & dependencies

- Table `watchlists`. Migrations `000002_create_watchlists`, `000003_add_watchlist_alerts`, `000006_add_watchlist_category`.
- `Add` validates the symbol against FMP `SearchSymbol`.

## Rules & gotchas

- `Add` stores the canonical symbol from FMP (it prefers an exact case-insensitive match). `name` and `type` come
  from the client. When `type` is missing, it falls back to FMP's type. A nil `category` is stored as `[]`.
- `SetAlerts` uses a `*bool` + `binding:"required"` so an explicit `false` passes validation.

## Tests

`handler/watchlist_test.go`, `usecase/watchlist/watchlist_test.go`
