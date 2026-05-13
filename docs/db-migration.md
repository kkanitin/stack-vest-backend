# Plan: Flyway-like Database Migration System

## Context

The project currently runs DDL migrations by calling `userRepo.Migrate()` on startup — hardcoded SQL statements inside the repository layer with no version tracking. This makes it impossible to know which changes have been applied, prevents safe schema evolution, and couples infrastructure concerns to the repository struct. The goal is to replace this with a proper versioned migration system (Flyway-style): numbered `.sql` files, a tracking table in the database, and only unapplied migrations executed on each startup. A config toggle allows disabling auto-migration for environments that manage schema separately.

---

## Approach

Use `golang-migrate/migrate/v4` with:
- **Source:** `iofs` (embed SQL files into the binary via `//go:embed`)
- **Driver:** `pgx/v5` (same driver the project already uses)
- **Tracking table:** `schema_migrations` (auto-created by golang-migrate)

Migration files live in `db/migrations/` at the project root and are embedded at compile time.

---

## File Changes

### 1. New: `db/migrations/` — 6 SQL files

Convert the 3 existing hardcoded DDL statements into versioned migration pairs.

**`000001_create_users.up.sql`**
```sql
CREATE TABLE IF NOT EXISTS users (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    google_id  TEXT        UNIQUE,
    email      TEXT        NOT NULL,
    name       TEXT        NOT NULL,
    picture    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

**`000001_create_users.down.sql`**
```sql
DROP TABLE IF EXISTS users;
```

**`000002_alter_users_google_id_nullable.up.sql`**
```sql
ALTER TABLE users ALTER COLUMN google_id DROP NOT NULL;
```

**`000002_alter_users_google_id_nullable.down.sql`**
```sql
ALTER TABLE users ALTER COLUMN google_id SET NOT NULL;
```

**`000003_add_users_email_unique.up.sql`**
```sql
ALTER TABLE users ADD CONSTRAINT IF NOT EXISTS users_email_key UNIQUE (email);
```

**`000003_add_users_email_unique.down.sql`**
```sql
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
```

> All three up-migrations are idempotent, so existing databases (where the table already exists from the old `Migrate()`) are safe — golang-migrate will create `schema_migrations` and run all three without error.

---

### 2. New: `pkg/migrate/migrate.go`

```go
package migrate

import (
    "embed"
    "errors"
    "fmt"
    "io/fs"
    "strings"

    "github.com/golang-migrate/migrate/v4"
    _ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
    "github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed ../../db/migrations/*.sql
var migrationFiles embed.FS

// Run applies all pending UP migrations. Returns nil when already up-to-date.
func Run(dsn string) error {
    dbURL := toPgx5DSN(dsn)

    sub, err := fs.Sub(migrationFiles, "db/migrations")
    if err != nil {
        return fmt.Errorf("migrate: sub fs: %w", err)
    }
    src, err := iofs.New(sub, ".")
    if err != nil {
        return fmt.Errorf("migrate: iofs source: %w", err)
    }

    m, err := migrate.NewWithSourceInstance("iofs", src, dbURL)
    if err != nil {
        return fmt.Errorf("migrate: init: %w", err)
    }
    defer m.Close()

    if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
        return fmt.Errorf("migrate: up: %w", err)
    }
    return nil
}

func toPgx5DSN(dsn string) string {
    if strings.HasPrefix(dsn, "postgres://") {
        return "pgx5://" + strings.TrimPrefix(dsn, "postgres://")
    }
    if strings.HasPrefix(dsn, "postgresql://") {
        return "pgx5://" + strings.TrimPrefix(dsn, "postgresql://")
    }
    return dsn
}
```

Key points:
- Blank import registers the `pgx5://` URL scheme driver via `init()`.
- `migrate.ErrNoChange` is swallowed — not an error condition.
- `defer m.Close()` releases the Postgres advisory lock even on early return.
- `//go:embed` path is relative to `pkg/migrate/migrate.go`, so `../../db/migrations/*.sql` resolves to the project-root `db/migrations/` directory.

---

### 3. Modify: `pkg/config/config.go`

**Add to `DB` struct** (after `Postgres`):
```go
Migrate struct {
    Enabled bool `yaml:"enabled"`
} `yaml:"migrate"`
```

**Add default in `Load()`** (before YAML unmarshal):
```go
cfg.DB.Migrate.Enabled = true
```

**Add env var override** (after `DB_POSTGRES_DSN` block):
```go
if v := os.Getenv("DB_MIGRATE_ENABLED"); v != "" {
    cfg.DB.Migrate.Enabled = parseBool(v)
}
```

**Add helper** (bottom of file, requires adding `"strings"` to imports):
```go
func parseBool(s string) bool {
    switch strings.ToLower(strings.TrimSpace(s)) {
    case "true", "1", "yes":
        return true
    default:
        return false
    }
}
```

---

### 4. Modify: `config.yaml.example`

Add under `db:`:
```yaml
db:
  postgres:
    dsn: "postgres://user:password@localhost:5432/stackvest?sslmode=disable"
  migrate:
    enabled: true   # set to false to skip auto-migration on startup
```

---

### 5. Modify: `main.go`

Replace lines 39–43 (the `userRepo.Migrate` block):

**Remove:**
```go
userRepo := userrepo.NewPostgresRepository(pool)
if err := userRepo.Migrate(context.Background()); err != nil {
    slog.Error("failed to run migrations", "error", err)
    os.Exit(1)
}
```

**Replace with:**
```go
if cfg.DB.Migrate.Enabled {
    slog.Info("running database migrations")
    if err := migrate.Run(cfg.DB.Postgres.DSN); err != nil {
        slog.Error("failed to run database migrations", "error", err)
        os.Exit(1)
    }
    slog.Info("database migrations complete")
}

userRepo := userrepo.NewPostgresRepository(pool)
```

Add import: `"github.com/kanitin/stackvest/backend/pkg/migrate"`

---

### 6. Modify: `internal/repository/user/postgres.go`

Remove the entire `Migrate()` method (lines 23–42). The `"context"` import remains needed by the other methods.

---

### 7. Modify: `AGENTS.md`

Add `DB_MIGRATE_ENABLED` row to the Environment Variables table:

```
| `DB_MIGRATE_ENABLED`                    | `db.migrate.enabled`                    | `true`  |
```

Also add the missing `DB_POSTGRES_DSN` row (currently implemented but absent from the table):

```
| `DB_POSTGRES_DSN`                       | `db.postgres.dsn`                       | —       |
```

---

## Dependencies to Install

```
go get github.com/golang-migrate/migrate/v4
go get github.com/golang-migrate/migrate/v4/database/pgx/v5
go get github.com/golang-migrate/migrate/v4/source/iofs
```

---

## Verification

1. `go build ./...` — confirms embed path resolves and all packages compile.
2. `go vet ./...` — static checks.
3. Start server against a fresh Postgres DB → `schema_migrations` table created, version `3` recorded, `users` table present with correct schema.
4. Restart server → "database migrations complete" logged, no SQL errors, `schema_migrations.dirty = false`.
5. Set `DB_MIGRATE_ENABLED=false` → restart, "running database migrations" log line absent.
6. Existing DB (table already present from old code) → all three up-migrations run idempotently without error.