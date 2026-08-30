// Package db provides the SQLite schema, connection setup, and admin seeding
// used at server startup.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
	// Blank import to register the modernc.org/sqlite driver with database/sql.
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT 'client' CHECK (role IN ('client','admin')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS cars (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
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

	if _, err := d.ExecContext(context.Background(), schema); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// OverlapPredicate is a SQL fragment that detects overlapping reservations
// over a range [start, end]. The reservations table must be aliased as r.
const OverlapPredicate = "r.start_date <= ? AND r.end_date >= ?"

// SeedAdmin creates the admin user with the given email and password if it
// does not exist, or updates its password when it already exists. It returns
// an error when the email belongs to an existing non-admin account.
func SeedAdmin(d *sql.DB, email, password string) error {
	if email == "" || password == "" {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	ctx := context.Background()
	var role string
	err = d.QueryRowContext(ctx, `SELECT role FROM users WHERE email = ?`, email).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = d.ExecContext(ctx, `INSERT INTO users (email, password_hash, role) VALUES (?, ?, 'admin')`, email, string(hash))
		return err
	}
	if err != nil {
		return err
	}
	if role != "admin" {
		return fmt.Errorf("ADMIN_EMAIL %q conflicts with an existing %s account", email, role)
	}

	_, err = d.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE email = ? AND role = 'admin'`, string(hash), email)
	return err
}
