package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationsApplied(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM goose_db_version WHERE version_id = 1 AND is_applied = 1`).Scan(&n); err != nil {
		t.Fatalf("goose_db_version: %v", err)
	}
	if n != 1 {
		t.Fatalf("migration 00001 not applied: rows = %d, want 1", n)
	}
}

func TestPerfMigrationApplied(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM goose_db_version WHERE version_id = 2 AND is_applied = 1`).Scan(&n); err != nil {
		t.Fatalf("goose_db_version: %v", err)
	}
	if n != 1 {
		t.Fatalf("migration 00002 not applied: rows = %d, want 1", n)
	}
}

func TestIndexesExist(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	want := map[string]bool{
		"idx_cars_owner":             false,
		"idx_reservations_user":      false,
		"idx_reservations_car_dates": false,
	}
	for _, table := range []string{"cars", "reservations"} {
		rows, err := d.QueryContext(context.Background(),
			`SELECT name FROM pragma_index_list(?)`, table)
		if err != nil {
			t.Fatalf("index_list(%s): %v", table, err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if _, ok := want[name]; ok {
				want[name] = true
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
		_ = rows.Close()
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing index %q", name)
		}
	}
}

func TestPragmasConfigured(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "pragmas.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	readint := func(pragma string) int {
		t.Helper()
		var v int
		if err := d.QueryRowContext(context.Background(),
			`PRAGMA `+pragma).Scan(&v); err != nil {
			t.Fatalf("PRAGMA %s: %v", pragma, err)
		}
		return v
	}

	if v := readint("journal_size_limit"); v != 67108864 {
		t.Fatalf("journal_size_limit = %d, want 67108864", v)
	}
	if v := readint("synchronous"); v != 1 {
		t.Fatalf("synchronous = %d, want 1 (NORMAL)", v)
	}
	if v := readint("busy_timeout"); v != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", v)
	}
	if v := readint("foreign_keys"); v != 1 {
		t.Fatalf("foreign_keys = %d, want 1", v)
	}

	var mode string
	if err := d.QueryRowContext(context.Background(),
		`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestSchemaHasOwnerID(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	rows, err := d.QueryContext(context.Background(), `PRAGMA table_info(cars)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()

	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "owner_id" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if !found {
		t.Fatal("cars table missing owner_id column")
	}
}

func TestMigrateLegacySchemaKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	const legacySchema = `
CREATE TABLE users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role TEXT NOT NULL DEFAULT 'client' CHECK (role IN ('client','admin')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE cars (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	owner_id INTEGER NOT NULL REFERENCES users(id),
	name TEXT NOT NULL,
	photo_url TEXT NOT NULL DEFAULT '',
	price_per_day INTEGER NOT NULL CHECK (price_per_day >= 0),
	active INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE reservations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(id),
	car_id INTEGER NOT NULL REFERENCES cars(id),
	start_date TEXT NOT NULL, end_date TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','cancelled')),
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE payments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	reservation_id INTEGER NOT NULL UNIQUE REFERENCES reservations(id),
	method TEXT NOT NULL CHECK (method IN ('pos','cash')),
	status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
	proof_url TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO users (email, password_hash, role) VALUES ('admin@example.com', 'h', 'admin');
`
	if _, err := legacy.ExecContext(context.Background(), legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	d, err := Open(path)
	if err != nil {
		t.Fatalf("open after migrate: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	var n int
	if err := d.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM goose_db_version WHERE version_id = 1 AND is_applied = 1`).Scan(&n); err != nil {
		t.Fatalf("goose_db_version: %v", err)
	}
	if n != 1 {
		t.Fatalf("migration 00001 not applied: rows = %d, want 1", n)
	}

	// Migrations must be non-destructive: legacy rows survive the upgrade.
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 1 {
		t.Fatalf("legacy rows lost after migrate: users = %d, want 1", n)
	}
}

func TestRoleConstraint(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	for _, role := range []string{"buyer", "seller"} {
		if _, err := d.ExecContext(context.Background(),
			`INSERT INTO users (email, password_hash, role) VALUES (?, 'h', ?)`,
			role+"@example.com", role); err != nil {
			t.Fatalf("insert %s: %v", role, err)
		}
	}

	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO users (email, password_hash, role) VALUES ('admin@example.com', 'h', 'admin')`); err == nil {
		t.Fatal("expected role constraint to reject 'admin'")
	}
}

func TestCarOwnerRequired(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO users (email, password_hash, role) VALUES ('seller@example.com', 'h', 'seller')`); err != nil {
		t.Fatalf("insert seller: %v", err)
	}
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO cars (owner_id, name, price_per_day) VALUES (1, 'Yaris', 45000)`); err != nil {
		t.Fatalf("insert car with owner: %v", err)
	}
	if _, err := d.ExecContext(context.Background(),
		`INSERT INTO cars (name, price_per_day) VALUES ('NoOwner', 45000)`); err == nil {
		t.Fatal("expected NOT NULL violation for missing owner_id")
	}
}

func TestEmailCaseInsensitiveUnique(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	ctx := context.Background()
	if _, err := d.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ('User@Example.COM', 'h', 'buyer')`); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// The case-insensitive unique index (lower(email)) must reject a
	// second row that differs only in casing.
	_, err = d.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ('user@example.com', 'h', 'buyer')`)
	if err == nil {
		t.Fatal("expected UNIQUE violation for case-different email")
	}
}
