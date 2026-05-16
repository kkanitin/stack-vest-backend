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

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Run applies all pending UP migrations. Returns nil when already up-to-date.
func Run(dsn string) error {
	dbURL := toPgx5DSN(dsn)

	sub, err := fs.Sub(migrationFiles, "migrations")
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

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		// Dirty state means a previous run failed mid-migration. Reset to the
		// last clean version and retry so the failed migration runs again.
		var dirtyErr migrate.ErrDirty
		if errors.As(err, &dirtyErr) {
			if forceErr := m.Force(dirtyErr.Version - 1); forceErr != nil {
				return fmt.Errorf("migrate: force version: %w", forceErr)
			}
			if retryErr := m.Up(); retryErr != nil && !errors.Is(retryErr, migrate.ErrNoChange) {
				return fmt.Errorf("migrate: up: %w", retryErr)
			}
			return nil
		}
		// DB version is ahead of available source files (e.g. after consolidating
		// migrations). Reset to no version and re-apply; all migrations are idempotent.
		if strings.Contains(err.Error(), "no migration found for version") {
			if forceErr := m.Force(-1); forceErr != nil {
				return fmt.Errorf("migrate: force version: %w", forceErr)
			}
			if retryErr := m.Up(); retryErr != nil && !errors.Is(retryErr, migrate.ErrNoChange) {
				return fmt.Errorf("migrate: up: %w", retryErr)
			}
			return nil
		}
		return fmt.Errorf("migrate: up: %w", err)
	}
	return nil
}

func toPgx5DSN(dsn string) string {
	var url string
	if strings.HasPrefix(dsn, "postgres://") {
		url = "pgx5://" + strings.TrimPrefix(dsn, "postgres://")
	} else if strings.HasPrefix(dsn, "postgresql://") {
		url = "pgx5://" + strings.TrimPrefix(dsn, "postgresql://")
	} else {
		url = dsn
	}
	if strings.Contains(url, "?") {
		url += "&search_path=stackvest"
	} else {
		url += "?search_path=stackvest"
	}
	return url
}
