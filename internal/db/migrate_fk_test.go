package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrationWithExistingReservations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fk.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	schema := `
CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'buyer' CHECK (role IN ('buyer','seller')), created_at TEXT NOT NULL DEFAULT (datetime('now')));
CREATE TABLE cars (id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id INTEGER NOT NULL REFERENCES users(id), name TEXT NOT NULL, photo_url TEXT NOT NULL DEFAULT '', price_per_day INTEGER NOT NULL CHECK (price_per_day >= 0), active INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT (datetime('now')));
CREATE TABLE reservations (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id), car_id INTEGER NOT NULL REFERENCES cars(id), start_date TEXT NOT NULL, end_date TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','cancelled')), created_at TEXT NOT NULL DEFAULT (datetime('now')));
CREATE TABLE payments (id INTEGER PRIMARY KEY AUTOINCREMENT, reservation_id INTEGER NOT NULL UNIQUE REFERENCES reservations(id), method TEXT NOT NULL CHECK (method IN ('pos','cash')), status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')), proof_url TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT (datetime('now')));
`
	if _, err := legacy.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(),
		`INSERT INTO users (email,password_hash,role) VALUES ('s@example.com','h','seller')`); err != nil {
		t.Fatalf("insert seller: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(),
		`INSERT INTO users (email,password_hash,role) VALUES ('b@example.com','h','buyer')`); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(),
		`INSERT INTO cars (owner_id,name,price_per_day) VALUES (1,'Yaris',50000)`); err != nil {
		t.Fatalf("insert car: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(),
		`INSERT INTO reservations (user_id,car_id,start_date,end_date) VALUES (2,1,'2026-10-01','2026-10-05')`); err != nil {
		t.Fatalf("insert reservation: %v", err)
	}
	_ = legacy.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatalf("migrate with FK rows should succeed: %v", err)
	}
	defer func() { _ = d.Close() }()
	var n int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM reservations`).Scan(&n); err != nil {
		t.Fatalf("count res: %v", err)
	}
	if n != 1 {
		t.Fatalf("reservations lost: %d", n)
	}
	var fkErr int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&fkErr); err == nil && fkErr != 0 {
		t.Fatalf("FK check failed: %d", fkErr)
	}
}

func TestMigrationSanitizesDirtyData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dirty.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	schema := `
CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'buyer' CHECK (role IN ('buyer','seller')), created_at TEXT NOT NULL DEFAULT (datetime('now')));
CREATE TABLE cars (id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id INTEGER NOT NULL REFERENCES users(id), name TEXT NOT NULL, photo_url TEXT NOT NULL DEFAULT '', price_per_day INTEGER NOT NULL CHECK (price_per_day >= 0), active INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT (datetime('now')));
CREATE TABLE reservations (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id), car_id INTEGER NOT NULL REFERENCES cars(id), start_date TEXT NOT NULL, end_date TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','confirmed','cancelled')), created_at TEXT NOT NULL DEFAULT (datetime('now')));
CREATE TABLE payments (id INTEGER PRIMARY KEY AUTOINCREMENT, reservation_id INTEGER NOT NULL UNIQUE REFERENCES reservations(id), method TEXT NOT NULL CHECK (method IN ('pos','cash')), status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')), proof_url TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT (datetime('now')));
`
	if _, err := legacy.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(),
		`INSERT INTO users (email,password_hash,role) VALUES ('s@example.com','h','seller')`); err != nil {
		t.Fatalf("insert seller: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(),
		`INSERT INTO users (email,password_hash,role) VALUES ('b@example.com','h','buyer')`); err != nil {
		t.Fatalf("insert buyer: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(),
		`INSERT INTO cars (owner_id,name,price_per_day) VALUES (1,'Overflow',150000000)`); err != nil {
		t.Fatalf("insert dirty car: %v", err)
	}
	if _, err := legacy.ExecContext(context.Background(),
		`INSERT INTO reservations (user_id,car_id,start_date,end_date) VALUES (2,1,'2026-10-10','2026-10-05')`); err != nil {
		t.Fatalf("insert dirty res: %v", err)
	}
	_ = legacy.Close()

	d, err := Open(path)
	if err != nil {
		t.Fatalf("migrate dirty should succeed (saneado): %v", err)
	}
	defer func() { _ = d.Close() }()
	var price int
	if err := d.QueryRowContext(context.Background(), `SELECT price_per_day FROM cars WHERE id=1`).Scan(&price); err != nil {
		t.Fatalf("price: %v", err)
	}
	if price != 100000000 {
		t.Fatalf("price not clamped: %d", price)
	}
	var startD, endD string
	if err := d.QueryRowContext(context.Background(), `SELECT start_date,end_date FROM reservations WHERE id=1`).Scan(&startD, &endD); err != nil {
		t.Fatalf("dates: %v", err)
	}
	if endD < startD {
		t.Fatalf("dates not sanitized: %s < %s", endD, startD)
	}
}
