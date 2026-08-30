// Package db provides the SQLite schema, connection setup, and versioned
// migration used at server startup.
package db

import (
	"context"
	"database/sql"
	"fmt"

	// Blank import to register the modernc.org/sqlite driver with database/sql.
	_ "modernc.org/sqlite"
)

// schemaVersion is the current database schema version, tracked via PRAGMA
// user_version. Versions older than this are rebuilt destructively by migrate.
const schemaVersion = 2

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT 'buyer' CHECK (role IN ('buyer','seller')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS cars (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	owner_id INTEGER NOT NULL REFERENCES users(id),
	name TEXT NOT NULL,
	photo_url TEXT NOT NULL DEFAULT '',
	price_per_day INTEGER NOT NULL CHECK (price_per_day >= 0),
	active INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS reservations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	car_id INTEGER NOT NULL REFERENCES cars(id),
	start_date TEXT NOT NULL,
	end_date TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','cancelled')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS payments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	reservation_id INTEGER NOT NULL UNIQUE REFERENCES reservations(id),
	method TEXT NOT NULL CHECK (method IN ('pos','cash')),
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
	proof_url TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`

// Open returns a SQLite database pool configured for the connection URL.
// In-memory databases are limited to a single connection; file databases use
// WAL mode with a small pool and a 5s busy timeout.
func Open(url string) (*sql.DB, error) {
	mem := url == ":memory:"
	var dsn string
	if mem {
		dsn = "file::memory:?cache=shared"
	} else {
		dsn = fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", url)
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
	}

	if err := migrate(d); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// migrate brings the database up to schemaVersion. Databases with an older
// schema are rebuilt: the schema predates any production data, so a destructive
// rebuild keeps local development databases working. Fresh databases (version
// 0 with no tables) just run the CREATE statements.
func migrate(d *sql.DB) error {
	ctx := context.Background()

	var v int
	if err := d.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
		return fmt.Errorf("migrate: read version: %w", err)
	}
	if v >= schemaVersion {
		return nil
	}

	if v > 0 {
		fmt.Printf("migrate: rebuilding database schema v%d -> v%d\n", v, schemaVersion)
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS payments`,
		`DROP TABLE IF EXISTS reservations`,
		`DROP TABLE IF EXISTS cars`,
		`DROP TABLE IF EXISTS users`,
	} {
		if _, err := d.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if _, err := d.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if _, err := d.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion)); err != nil {
		return fmt.Errorf("migrate: set version: %w", err)
	}
	return nil
}

// OverlapPredicate is a SQL fragment that detects overlapping reservations
// over a range [start, end]. The reservations table must be aliased as r.
const OverlapPredicate = "r.start_date <= ? AND r.end_date >= ?"
