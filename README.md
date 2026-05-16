# StackVest Backend

Go REST API for the StackVest platform, built with [Gin](https://github.com/gin-gonic/gin) and PostgreSQL following
Clean Architecture principles.

## Requirements

- Go 1.26.2+
- PostgreSQL

## Getting Started

1. Copy the config file and fill in your values:
   ```bash
   cp config.yaml.example config.yaml
   ```

2. Run the server:
   ```bash
   go run main.go
   ```

The server starts on `:8080` by default. Database migrations run automatically on startup.

## Database Migrations

Migrations are managed with [golang-migrate](https://github.com/golang-migrate/migrate) and versioned SQL files embedded
in the binary at `pkg/migrate/migrations/`.

**How it works:**

- On startup, all pending `.up.sql` files are applied in version order.
- Applied versions are tracked in a `schema_migrations` table that golang-migrate creates automatically.
- Migrations are idempotent — restarting the server when already up-to-date is safe.

**Adding a migration:**

1. Create a new pair of files in `pkg/migrate/migrations/`:
   ```
   000004_your_description.up.sql
   000004_your_description.down.sql
   ```
2. Write the forward change in `.up.sql` and the rollback in `.down.sql`.
3. Restart the server — the migration runs automatically.

**Disabling auto-migration:**

Set `DB_MIGRATE_ENABLED=false` (or `db.migrate.enabled: false` in `config.yaml`) to skip migrations on startup. Useful
in environments where schema changes are managed separately.

## Environment Variables

See [AGENTS.md](./AGENTS.md#environment-variables) for the full list. Key ones:

| Variable             | Default | Description                  |
|----------------------|---------|------------------------------|
| `SERVER_PORT`        | `8080`  | HTTP listen port             |
| `DB_POSTGRES_DSN`    | —       | PostgreSQL connection string |
| `DB_MIGRATE_ENABLED` | `true`  | Run migrations automatically |

## Project Structure

```
internal/
  domain/             # Entities and repository/usecase interfaces
  usecase/            # Business logic
  repository/         # PostgreSQL implementations
  delivery/http/
    handler/          # Gin request handlers
    router/           # Route registration
    middleware/       # Gin middleware

pkg/
  config/             # Environment config
  database/           # PostgreSQL client setup
  migrate/            # Migration runner + embedded SQL files
    migrations/       # Versioned .up.sql / .down.sql files
```

## Development

```bash
go build -o bin/backend .   # build
go test ./...               # run tests
go vet ./...                # lint
```