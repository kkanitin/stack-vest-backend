# AGENTS.md

This file provides guidance to AI agents (Claude Code, Junie, etc.) when working with code in this repository.
It contains **repo-wide rules only**. Feature-specific knowledge lives in `docs/features/`; reference material lives
in the `docs/` tree, indexed by [`docs/index.md`](./docs/index.md).

## Documentation

**Before changing code, read [`docs/index.md`](./docs/index.md) first** and then the doc(s) it points to for the area
you're working in. It indexes the reference docs (architecture, HTTP conventions, logging, configuration) and the
feature docs.

**Before implementing a new feature or adjusting an existing one, also read [`docs/features/index.md`](./docs/features/index.md)**
and the feature doc(s) it links to. It lists every feature, its endpoints, its main packages, and the dependencies
between features.

**Keep docs in sync with every change.** Any change that affects documented behavior must update the matching docs
in the same change. This includes bug fixes, refactors, config changes and dependency upgrades, not only new
features. Documented behavior covers endpoints, request/response shapes, error codes, file locations, tables and
migrations, config keys, caching and external APIs:

- Feature-specific changes → the feature's doc in `docs/features/` and its row in `index.md`.
- Repo-wide changes (conventions, layout, commands, env vars, logging, dependencies) → this file, or the matching
  reference doc listed in `docs/index.md`.

A change is not complete while its docs are stale. Don't put feature-specific details in this file.

## Commands

```bash
# Build
go build -o bin/backend .

# Run
go run main.go          # starts on :8080
./bin/backend           # run compiled binary

# Test
go test ./...           # all tests
go test ./path/pkg/...  # single package

# Lint / vet
go vet ./...
```

## Git Policy

- **NEVER** perform `git commit` or `git push` commands.
- Read-only commands such as `git fetch`, `git status`, `git diff`, `git log`, etc. are permitted.
- Project modifications should only be made to files; the user will handle version control.

## Architecture

Go REST API for the **StackVest** platform using **Clean Architecture**. Dependencies point inward only: delivery →
usecase → domain. Frameworks (Gin, PostgreSQL, Redis) are confined to the outermost layers. The entry point is
`main.go`. The layer map and the steps for adding a feature are in
[`docs/architecture.md`](./docs/architecture.md).

## Rules to always follow

- **Response envelope:** all handlers (except `/health` and SSE endpoints) use the `internal/delivery/http/response/`
  helpers, never raw `gin.H`. JSON fields are `lowerCamelCase`. Details: `docs/http-conventions.md`.
- **Routes:** each handler owns its routes via `RegisterRoutes(*gin.RouterGroup)`; `router.go` only calls them.
- **Validation:** use `binding:"..."` struct tags, not ad-hoc checks.
- **Ownership:** for user-owned resources addressed by ID, return `404` (never `403`) when missing or owned by someone else.
- **Logging:** log errors once, at the handler layer, with `logger.RequestID(ctx)` and `zap.Error(err)`. Never log
  secrets or PII. Details: `docs/logging.md`.
- **Graceful shutdown:** every resource holding a connection or background process must register a cleanup func in
  `runUntilShutdown` in `main.go`.
- **Config:** when `config.yaml` changes, update `config.yaml.example` in the same change. **NEVER put real secrets
  in `config.yaml.example`**, only descriptive placeholders. Env var names follow config path → uppercase with dots
  replaced by underscores (full table in `docs/configuration.md`).
