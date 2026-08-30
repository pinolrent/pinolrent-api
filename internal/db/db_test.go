package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSchemaVersion(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	var v int
	if err := d.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if v != schemaVersion {
		t.Fatalf("user_version = %d, want %d", v, schemaVersion)
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

func TestMigrateLegacySchema(t *testing.T) {
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

	var v int
	if err := d.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	if v != schemaVersion {
		t.Fatalf("user_version = %d, want %d", v, schemaVersion)
	}

	var n int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if n != 0 {
		t.Fatalf("legacy rows kept after rebuild: users = %d, want 0", n)
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