// Package db provides the SQLite connection setup and the goose migrations
// applied at server startup.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	// Blank import to register the modernc.org/sqlite driver with database/sql.
	_ "modernc.org/sqlite"

	"github.com/cenkalti/backoff/v4"
	"github.com/pressly/goose/v3"
)

// migrationsFS embeds the versioned SQL migrations applied by migrate.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open returns a SQLite database pool configured for the connection URL.
// In-memory databases are limited to a single connection; file databases use
// WAL mode with NORMAL synchronous commits, a bounded WAL, a small pool and a
// 5s busy timeout.
func Open(url string) (*sql.DB, error) {
	mem := url == ":memory:"
	var dsn string
	if mem {
		dsn = "file::memory:?cache=shared&_pragma=foreign_keys(1)"
	} else {
		dsn = fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=journal_size_limit(67108864)", url)
	}

	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if mem {
		d.SetMaxOpenConns(1)
	} else {
		d.SetMaxOpenConns(8)
		d.SetMaxIdleConns(8)
		d.SetConnMaxIdleTime(5 * time.Minute)
		d.SetConnMaxLifetime(30 * time.Minute)
	}

	if err := migrate(d); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// migrate brings the database schema up to date with the goose migrations
// embedded in the binary. Only pending migrations are applied; existing data
// is never dropped, unlike the destructive rebuild of earlier versions.
func migrate(d *sql.DB) error {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: embed migrations: %w", err)
	}
	prov, err := goose.NewProvider(goose.DialectSQLite3, d, sub)
	if err != nil {
		return fmt.Errorf("migrate: provider: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var applied []*goose.MigrationResult
	op := func() error {
		var opErr error
		applied, opErr = prov.Up(ctx)
		if opErr != nil && isBusyError(opErr) {
			return opErr
		}
		return backoff.Permanent(opErr)
	}
	exp := backoff.NewExponentialBackOff()
	exp.InitialInterval = 100 * time.Millisecond
	if err := backoff.Retry(op, backoff.WithContext(backoff.WithMaxRetries(exp, 5), ctx)); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	for _, m := range applied {
		slog.Info("migration applied", "version", m.Source.Version, "path", m.Source.Path)
	}
	return nil
}

// OverlapPredicate is a SQL fragment that detects overlapping reservations
// over a range [start, end]. The reservations table must be aliased as r.
const OverlapPredicate = "r.start_date <= ? AND r.end_date >= ?"

func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is busy") || strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_BUSY")
}
