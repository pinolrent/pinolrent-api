package handlers

import (
	"context"
	"testing"
)

// isUniqueViolation is exercised against real driver errors: the unique
// constraint on users.email and the CHECK on users.role produce different
// result codes, so detection must tell them apart without reading error text.
func TestIsUniqueViolation(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()

	insert := func() error {
		_, err := a.DB.ExecContext(ctx,
			`INSERT INTO users (email, password_hash, role) VALUES ('dup@example.com', 'h', 'buyer')`)
		return err
	}

	if err := insert(); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := insert(); err == nil {
		t.Fatal("second insert with same email should fail")
	} else if !isUniqueViolation(err) {
		t.Fatalf("expected UNIQUE violation, got: %v", err)
	}

	// A CHECK violation (role not in buyer/seller) must not be detected as
	// a unique violation.
	_, err := a.DB.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, role) VALUES ('other@example.com', 'h', 'admin')`)
	if err == nil {
		t.Fatal("expected CHECK violation for role 'admin'")
	}
	if isUniqueViolation(err) {
		t.Fatalf("CHECK violation must not be a unique violation, got: %v", err)
	}
}
