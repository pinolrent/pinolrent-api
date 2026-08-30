package db

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestSeedAdminCreateAndUpdate(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if err := SeedAdmin(d, "admin@example.com", "pw1"); err != nil {
		t.Fatalf("seed (1): %v", err)
	}
	if err := SeedAdmin(d, "admin@example.com", "pw2"); err != nil {
		t.Fatalf("seed (2): %v", err)
	}

	var hash string
	if err := d.QueryRowContext(context.Background(), `SELECT password_hash FROM users WHERE email = 'admin@example.com'`).Scan(&hash); err != nil {
		t.Fatalf("query: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("pw1")) == nil {
		t.Fatal("expected password to be updated on second seed")
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("pw2")) != nil {
		t.Fatal("updated password should match pw2")
	}
}

func TestSeedAdminCreateIdempotentCount(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	for i := 0; i < 3; i++ {
		if err := SeedAdmin(d, "admin@example.com", "pw"); err != nil {
			t.Fatalf("seed #%d: %v", i, err)
		}
	}
	var n int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE email = 'admin@example.com'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("admin rows = %d, want 1", n)
	}
}

func TestSeedAdminConflict(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if _, err := d.ExecContext(context.Background(), `INSERT INTO users (email, password_hash, role) VALUES ('client@example.com', 'h', 'client')`); err != nil {
		t.Fatalf("insert client: %v", err)
	}

	err = SeedAdmin(d, "client@example.com", "pw")
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSeedAdminEmpty(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if err := SeedAdmin(d, "", ""); err != nil {
		t.Fatalf("empty seed: %v", err)
	}
	var n int
	if err := d.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("users = %d, want 0", n)
	}
}
