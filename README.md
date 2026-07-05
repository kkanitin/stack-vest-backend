# StackVest Backend

REST API for **StackVest**, a personal investing platform — track portfolios, simulate
dollar-cost-averaging strategies, watch stocks with price alerts, follow the dividend calendar,
and get AI-generated portfolio analysis streamed in real time.

Built in **Go** with [Gin](https://github.com/gin-gonic/gin) and **PostgreSQL**, following Clean
Architecture: dependencies point inward only (`delivery → usecase → domain`), so frameworks and
external services stay confined to the outer layers.

## Features

- **Google OAuth 2.0 login** with JWT-based session auth
- **Portfolios** — full CRUD, positions, activity log, and computed value/return summaries
- **AI portfolio analysis** — streamed to the client over Server-Sent Events, backed by [Groq](https://groq.com)
- **DCA simulator** — model dollar-cost-averaging outcomes over time
- **Watchlist** — track symbols with configurable price alerts
- **Dividend calendar** — upcoming ex-dividend/payment dates, cached in Redis
- **Stock data** — search, quotes, price changes, history, and company profiles via [FMP](https://financialmodelingprep.com)
- **Market sentiment** and **popular stocks** endpoints
- **Rate limiting** — token-bucket, keyed per-IP for public routes and per-user for authenticated ones
- **Consistent API contract** — standard response envelope, `lowerCamelCase` JSON, and uniform pagination across all list endpoints
- **Operational polish** — structured JSON logging (`slog`), graceful shutdown, and automatic DB migrations on startup

## Tech Stack

| Concern         | Choice                                          |
|-----------------|-------------------------------------------------|
| Language        | Go 1.26+                                         |
| HTTP framework  | Gin                                              |
| Database        | PostgreSQL (`pgx`)                               |
| Cache           | Redis (`go-redis`)                               |
| Migrations      | golang-migrate (embedded SQL)                    |
| Auth            | Google OAuth 2.0 + JWT (`golang-jwt`)            |
| AI              | Groq (SSE streaming)                             |
| Market data     | Financial Modeling Prep (FMP)                    |
| Logging         | `log/slog`                                       |

## Requirements

- Go 1.26.2+
- PostgreSQL
- Redis (optional — the dividend calendar falls back to uncached reads when Redis is unavailable)

## Getting Started

1. Copy the config file and fill in your values:
   ```bash
   cp config.yaml.example config.yaml
   ```
   At minimum, set `db.postgres.dsn` and the `auth.google` / `auth.jwt` secrets. Every value can also
   be supplied via environment variable (see [Configuration](#configuration)).

2. Run the server:
   ```bash
   go run main.go
   ```

The server starts on `:8080` by default. Database migrations run automatically on startup.

## API

All endpoints are namespaced under `/api/v1`. `/health` and the routes below marked _public_ are
open; everything else requires a valid JWT (`Authorization: Bearer <token>`).

| Method | Path                                          | Auth    | Description                                  |
|--------|-----------------------------------------------|---------|----------------------------------------------|
| GET    | `/health`                                     | public  | Liveness/readiness probe                     |
| GET    | `/api/v1/auth/google`                         | public  | Begin Google OAuth login                     |
| GET    | `/api/v1/auth/google/callback`                | public  | OAuth callback → issues JWT                   |
| GET    | `/api/v1/popular`                             | public  | Popular / trending stocks                    |
| GET    | `/api/v1/users/me`                            | JWT     | Current user profile                         |
| POST   | `/api/v1/users/me`                            | JWT     | Create/upsert current user                   |
| GET    | `/api/v1/stocks/search`                       | JWT     | Symbol search                                |
| GET    | `/api/v1/stocks/:symbol/quote`                | JWT     | Latest quote                                 |
| GET    | `/api/v1/stocks/:symbol/history`              | JWT     | Historical prices                            |
| GET    | `/api/v1/stocks/:symbol/profile`              | JWT     | Company profile                              |
| GET    | `/api/v1/stocks/:symbol/price-change`         | JWT     | Price change for one symbol                  |
| GET    | `/api/v1/stocks/price-changes`                | JWT     | Batch price changes                          |
| GET    | `/api/v1/stocks/history`                      | JWT     | Batch history                                |
| GET    | `/api/v1/watchlist`                           | JWT     | List watchlist                               |
| POST   | `/api/v1/watchlist`                           | JWT     | Add symbol                                   |
| DELETE | `/api/v1/watchlist/:symbol`                   | JWT     | Remove symbol                                |
| PATCH  | `/api/v1/watchlist/:symbol/alerts`            | JWT     | Configure price alerts                       |
| POST   | `/api/v1/dca/simulate`                        | JWT     | Simulate a DCA strategy                      |
| GET    | `/api/v1/sentiment`                           | JWT     | Market sentiment                             |
| GET    | `/api/v1/dividends/calendar`                  | JWT     | Dividend calendar (Redis-cached)             |
| POST   | `/api/v1/portfolios`                          | JWT     | Create a portfolio                           |
| GET    | `/api/v1/portfolios`                          | JWT     | List portfolios                              |
| GET    | `/api/v1/portfolios/summary`                  | JWT     | Aggregate summary across portfolios          |
| POST   | `/api/v1/portfolios/analyze`                  | JWT     | **AI analysis, streamed as SSE**             |
| GET    | `/api/v1/portfolios/:id`                      | JWT     | Get one portfolio                            |
| PATCH  | `/api/v1/portfolios/:id`                      | JWT     | Update a portfolio                           |
| DELETE | `/api/v1/portfolios/:id`                      | JWT     | Delete a portfolio                           |
| GET    | `/api/v1/portfolios/:id/summary`              | JWT     | Value/return summary                         |
| GET    | `/api/v1/portfolios/:id/activity`             | JWT     | Activity log                                 |
| POST   | `/api/v1/portfolios/:id/positions`            | JWT     | Add a position                               |
| GET    | `/api/v1/portfolios/:id/positions`            | JWT     | List positions                               |
| PATCH  | `/api/v1/portfolios/:id/positions/:symbol`    | JWT     | Update a position                            |
| DELETE | `/api/v1/portfolios/:id/positions/:symbol`    | JWT     | Remove a position                            |
| POST   | `/api/v1/portfolios/:id/analyze`              | JWT     | AI analysis for one portfolio                |

Responses use a standard envelope (`result`/`results` + `code`/`message`/`errorMessage`, plus `meta`
for lists); the streaming analysis endpoints are the documented exception. See
[AGENTS.md](./AGENTS.md#standard-response-envelope) for the full contract.

## Configuration

Config is read from `config.yaml`, and any value can be overridden by an environment variable —
uppercase the config path and replace dots with underscores (`auth.google.client_id` →
`AUTH_GOOGLE_CLIENT_ID`). In production, set secrets via env vars and keep them out of `config.yaml`.

Key settings:

| Variable                        | Default | Description                        |
|---------------------------------|---------|------------------------------------|
| `SERVER_PORT`                   | `8080`  | HTTP listen port                   |
| `DB_POSTGRES_DSN`               | —       | PostgreSQL connection string       |
| `DB_MIGRATE_ENABLED`            | `true`  | Run migrations automatically       |
| `AUTH_GOOGLE_CLIENT_ID`         | —       | Google OAuth client ID             |
| `AUTH_GOOGLE_CLIENT_SECRET`     | —       | Google OAuth client secret         |
| `AUTH_JWT_SECRET`               | —       | JWT signing secret                 |
| `REDIS_ADDR`                    | —       | Redis host:port (dividend cache)   |
| `THIRD_PARTY_API_FMP_API_KEY`   | —       | Financial Modeling Prep API key    |
| `THIRD_PARTY_API_GROQ_API_KEY`  | —       | Groq API key (AI analysis)         |

See [AGENTS.md](./AGENTS.md#environment-variables) for the complete list.

## Database Migrations

Migrations are managed with [golang-migrate](https://github.com/golang-migrate/migrate) and versioned
SQL files embedded in the binary at `pkg/migrate/migrations/`.

**How it works:**

- On startup, all pending `.up.sql` files are applied in version order.
- Applied versions are tracked in a `schema_migrations` table that golang-migrate creates automatically.
- Migrations are idempotent — restarting the server when already up-to-date is safe.

**Adding a migration:**

1. Create a new pair of files in `pkg/migrate/migrations/`:
   ```
   000011_your_description.up.sql
   000011_your_description.down.sql
   ```
2. Write the forward change in `.up.sql` and the rollback in `.down.sql`.
3. Restart the server — the migration runs automatically.

**Disabling auto-migration:**

Set `DB_MIGRATE_ENABLED=false` (or `db.migrate.enabled: false` in `config.yaml`) to skip migrations on
startup. Useful in environments where schema changes are managed separately.

## Project Structure

```
internal/
  domain/             # Entities and repository/usecase interfaces (no external deps)
  usecase/            # Business logic; depends only on domain interfaces
  repository/         # PostgreSQL + Redis implementations of domain interfaces
  infrastructure/     # External API clients (FMP market data, Groq AI)
  delivery/http/
    handler/          # Gin request handlers
    router/           # Route registration
    middleware/       # Auth, rate limiting, logging, CORS
    response/         # Standard response envelope helpers

pkg/
  config/             # Config loading with env var overrides
  database/           # PostgreSQL client setup
  cache/              # Redis client setup
  logger/             # slog configuration
  migrate/            # Migration runner + embedded SQL files
    migrations/       # Versioned .up.sql / .down.sql files
```

## Development

```bash
go build -o bin/backend .   # build
go test ./...               # run tests
go vet ./...                # vet
```

Contributor and architecture conventions (Clean Architecture rules, the response envelope, validation,
pagination, logging) live in [AGENTS.md](./AGENTS.md).
