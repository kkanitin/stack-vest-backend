# AGENTS.md

This file provides guidance to AI agents (Claude Code, Junie, etc.) when working with code in this repository.

## Commands

```bash
# Build
go build -o backend .

# Run
go run main.go          # starts on :8080
./backend               # run compiled binary

# Test
go test ./...           # all tests
go test ./path/pkg/...  # single package

# Lint / vet
go vet ./...
```

## Architecture

Go REST API for the **StackVest** platform using **Clean Architecture**. Dependencies point inward only: delivery →
usecase → domain. Frameworks (Gin, MongoDB) are confined to the outermost layers.

## Git Policy

- **NEVER** perform `git commit` or `git push` commands.
- Read-only commands such as `git fetch`, `git status`, `git diff`, `git log`, etc. are permitted.
- Project modifications should only be made to files; the user will handle version control.

**Entry point:** `main.go` — loads config, wires the router, and starts the server.

**Layer map:**

```
internal/
  domain/               — Entities and repository/usecase interfaces (no external deps)
  usecase/              — Business logic; depends only on domain interfaces
  repository/           — MongoDB implementations of domain repository interfaces
  infrastructure/       — External API clients and other infrastructure (AlphaVantage)
  delivery/http/
    handler/            — Gin handlers; call use cases, never touch MongoDB directly
    router/             — Route registration; wires handlers together
    middleware/         — Auth, logging, and other Gin middleware

pkg/
  config/               — Config from config.yaml with env var overrides (see Environment Variables below)
  database/             — PostgreSQL connection pool setup
```

**Graceful shutdown rule:** every resource that holds a connection or runs a background process (e.g. Redis, message queue consumer, background worker) must register a cleanup func in the `runUntilShutdown` call in `main.go`. Each func receives a context with a 10-second deadline and is called in the order listed. Never leave a resource unregistered — unclean shutdowns cause connection leaks and data loss.

**Config file rule:** whenever `config.yaml` is modified (keys added, renamed, or removed), `config.yaml.example` must be updated in the same change. `config.yaml` is git-ignored; `config.yaml.example` is the committed reference that other developers copy to get started.

**NEVER put real secrets in `config.yaml.example`** — no real API keys, passwords, tokens, or credentials of any kind. Use descriptive placeholders only (e.g. `"YOUR_ALPHA_VANTAGE_API_KEY"`, `"YOUR_JWT_SECRET_CHANGE_ME"`). `config.yaml.example` is committed to the repository and publicly visible.

**Adding a feature** (e.g. `user`):

1. `internal/domain/` — define entity struct + repository interface
2. `internal/usecase/` — implement use case that accepts the repository interface
3. `internal/repository/` — implement the MongoDB repository
4. `internal/delivery/http/handler/` — Gin handler with a `RegisterRoutes(rg *gin.RouterGroup)` method that registers
   its own routes
5. `internal/delivery/http/router/router.go` — call `handler.RegisterRoutes(v1)` in `New()`

**Route registration convention:** each handler owns its routes via a `RegisterRoutes(*gin.RouterGroup)` method.
`router.go` stays thin — it only calls each handler's `RegisterRoutes`. Never inline route registration inside
`router.go`.

**API response convention:** all JSON response fields use `lowerCamelCase` (e.g. `marketOpen`, `matchScore`). Apply this to all `json:"..."` struct tags.

## Environment Variables

All config values can be overridden at runtime via environment variables. The naming rule is:
**config path → uppercase, dots replaced by underscores** (e.g. `auth.google.client_id` → `AUTH_GOOGLE_CLIENT_ID`).

| Env var                                 | `config.yaml` key                       | Default |
|-----------------------------------------|-----------------------------------------|---------|
| `SERVER_PORT`                           | `server.port`                           | `8080`  |
| `LOG_LEVEL`                             | `log.level`                             | `info`  |
| `LOG_FORMAT`                            | `log.format`                            | `json`  |
| `DB_POSTGRES_DSN`                       | `db.postgres.dsn`                       | —       |
| `AUTH_GOOGLE_CLIENT_ID`                 | `auth.google.client_id`                 | —       |
| `AUTH_GOOGLE_CLIENT_SECRET`             | `auth.google.client_secret`             | —       |
| `AUTH_GOOGLE_REDIRECT_URL`              | `auth.google.redirect_url`              | —       |
| `AUTH_JWT_SECRET`                       | `auth.jwt.secret`                       | —       |
| `THIRD_PARTY_API_ALPHA_VANTAGE_API_KEY` | `third_party_api.alpha_vantage.api_key` | —       |

Env vars take precedence over `config.yaml`. In production, set secrets via env vars and omit them from `config.yaml`
entirely.

## Logging

**Package:** `pkg/logger` wraps Go's standard `log/slog`. `main.go` calls `slog.SetDefault` once at startup so all code can use the package-level `slog.Info/Warn/Error/Debug` functions without carrying a logger reference.

**Configuration** (env vars or `config.yaml`):

| Env var | `config.yaml` key | Default | Values |
|---|---|---|---|
| `LOG_LEVEL` | `log.level` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `log.format` | `json` | `json`, `text` |

Use `text` locally for human-readable output; keep `json` in staging/production for log aggregators.

**Where to log:**

- **Errors only at the handler layer.** Use `slog.ErrorContext(c.Request.Context(), "short description", "error", err)`. Do not log the same error at multiple layers — wrap it with `fmt.Errorf` up the call chain, then log once at the top.
- **Do not log in use-case or repository layers** unless the error is swallowed (not returned). If an error is returned to the caller, the caller logs it.
- **HTTP requests** are logged automatically by `middleware.Logger` (method, path, status, latency, client IP). Do not duplicate request/response logging in handlers.
- **Startup events** (server starting, DB connected) go in `main.go` with `slog.Info`.

**Attribute conventions:**

```go
// Preferred: typed helpers
slog.ErrorContext(ctx, "payment failed", "orderID", id, "error", err)
slog.InfoContext(ctx, "user upserted", "userID", user.ID)

// Use slog.ErrorContext / slog.WarnContext / slog.InfoContext / slog.DebugContext
// (context-aware forms) inside handlers. Use slog.Error etc. in main.go where
// no context is available.
```

- Key names: `lowerCamelCase` (`userID`, `orderID`, `error`).
- Always include `"error", err` for error logs.
- Never log secrets, tokens, passwords, or PII.

**Key dependencies:**

- `gin-gonic/gin` — HTTP routing and middleware
- `go.mongodb.org/mongo-driver/v2` — MongoDB persistence
- `quic-go/quic-go` — HTTP/3 (QUIC) support

Module path: `github.com/kanitin/stackvest/backend`