# Architecture

Read this before adding a feature or moving code between layers.

Go REST API for the **StackVest** platform using **Clean Architecture**. Dependencies point inward only: delivery →
usecase → domain. Frameworks (Gin, PostgreSQL, Redis) are confined to the outermost layers.

**Entry point:** `main.go` loads config, wires the router, and starts the server.

## Layer map

```
internal/
  domain/               — Entities and repository/usecase interfaces (no external deps)
  usecase/              — Business logic; depends only on domain interfaces
  repository/           — PostgreSQL + Redis implementations of domain repository interfaces
  infrastructure/       — External API clients, caching decorators, and other infrastructure
  delivery/http/
    handler/            — Gin handlers; call use cases, never touch the database directly
    router/             — Route registration; wires handlers together
    middleware/         — Auth, logging, and other Gin middleware
    response/           — Standard response envelope helpers

pkg/
  config/               — Config from config.yaml with env var overrides (see configuration.md)
  database/             — PostgreSQL client setup
  cache/                — Redis client setup and in-memory TTL caches
  migrate/              — Embedded SQL migrations
  logger/               — zap logger setup and helpers
  requestid/            — Per-request ID generation and context helpers
  worker/               — Background jobs that run alongside the HTTP server
```

## Background jobs

`worker.StartPeriodic(interval, fn)` runs `fn` once at start and then on the interval, never overlapping, and passes
it a context that is cancelled on shutdown. Jobs are started in `main.go` next to the use case they call. There are two
jobs: the three-hourly value snapshot (see [features/portfolio.md](./features/portfolio.md#value-snapshots)) and the
index heatmap refresh (see [features/market-heatmap.md](./features/market-heatmap.md)).

A job has no request, so it logs its own outcome in the `main.go` callback: one `Error` on failure, one `Info` on
success.

## Graceful shutdown

Every resource that holds a connection or runs a background process (e.g. Redis, message queue consumer, background worker) must register a cleanup func in the `runUntilShutdown` call in `main.go`. Each func receives a context with a 10-second deadline and is called in the order listed, so a job's `Stop` must be listed before the resources it uses (the value snapshot job stops before `pool.Close()`). Never leave a resource unregistered — unclean shutdowns cause connection leaks and data loss.

## Adding a feature

For a new feature (e.g. `user`):

1. `internal/domain/`: define the entity struct and the repository interface
2. `internal/usecase/`: implement the use case, which accepts the repository interface
3. `internal/repository/`: implement the PostgreSQL repository
4. `internal/delivery/http/handler/`: write a Gin handler with a `RegisterRoutes(rg *gin.RouterGroup)` method that
   registers its own routes
5. `internal/delivery/http/router/router.go`: call `handler.RegisterRoutes(...)` in `New()`, on the public `v1` group
   or the `protected` group
6. `docs/features/`: create `<feature>.md` from the template in `docs/features/index.md` and add a row to the index

## Key dependencies

- `gin-gonic/gin` — HTTP routing and middleware
- `jackc/pgx/v5` — PostgreSQL driver
- `redis/go-redis/v9` — Redis client
- `golang-migrate/migrate/v4` — embedded SQL migrations
- `golang-jwt/jwt/v5` + `golang.org/x/oauth2` — JWT sessions and Google OAuth
- `go.uber.org/zap` — structured logging

Module path: `github.com/kanitin/stackvest/backend`
