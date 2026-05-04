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
  config/               — Config from config.yaml and Env (SERVER_PORT, MONGO_URI)
  database/             — MongoDB client setup
```

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

**Key dependencies:**

- `gin-gonic/gin` — HTTP routing and middleware
- `go.mongodb.org/mongo-driver/v2` — MongoDB persistence
- `quic-go/quic-go` — HTTP/3 (QUIC) support

Module path: `github.com/kanitin/stackvest/backend`