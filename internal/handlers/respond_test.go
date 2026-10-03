package handlers

import (
	"context"
	"testing"
)

// isUniqueViolation is exercised against real driver errors: the unique
// constraint on users.email and the CHECK on user_roles.role produce different
// result codes, so detection must tell them apart without reading error text.
func TestIsUniqueViolation(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()

	insert := func() error {
		_, err := a.DB.ExecContext(ctx,
			`INSERT INTO users (email, password_hash) VALUES ('dup@example.com', 'h')`)
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

	// A CHECK violation (role outside the allowed set) must not be detected
	// as a unique violation.
	_, err := a.DB.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, role) VALUES (1, 'moderator')`)
	if err == nil {
		t.Fatal("expected CHECK violation for role 'moderator'")
	}
	if isUniqueViolation(err) {
		t.Fatalf("CHECK violation must not be a unique violation, got: %v", err)
	}
}
