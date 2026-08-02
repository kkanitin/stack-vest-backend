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
		// migrations). Several migrations do non-idempotent DDL (bare ADD COLUMN /
		// CREATE TABLE, no IF NOT EXISTS), so blindly forcing to -1 and replaying
		// everything is guaranteed to fail on an already-provisioned DB and leaves the
		// tracking table dirty, crash-looping on every restart. Surface the original
		// error instead so an operator can investigate and resolve it deliberately.
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
	// lock_timeout bounds the pg_advisory_lock that golang-migrate's driver takes
	// inside ensureVersionTable. That one is called directly by the driver rather
	// than through migrate's own lock timeout, and its source documents it as
	// waiting indefinitely — so without this a concurrent or stuck migration run
	// hangs forever with no further log output.
	params := "search_path=stackvest&lock_timeout=15000"
	if strings.Contains(url, "?") {
		url += "&" + params
	} else {
		url += "?" + params
	}
	return url
}
