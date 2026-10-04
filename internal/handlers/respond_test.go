package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// TestWriteTxErrBusyAsksRetry pins the contention contract: a SQLite
// busy/locked condition leaving a transaction answers 503 with Retry-After
// (retry), not 500 (bug). The busy error is a real driver one, produced like
// in db's is-busy test: an exclusive lock plus a zero busy timeout.
func TestWriteTxErrBusyAsksRetry(t *testing.T) {
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "busy.db")+"?_pragma=busy_timeout(0)")
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer func() { _ = raw.Close() }()

	ctx := context.Background()
	connA, err := raw.Conn(ctx)
	if err != nil {
		t.Fatalf("conn A: %v", err)
	}
	defer func() { _ = connA.Close() }()
	if _, err := connA.ExecContext(ctx, `CREATE TABLE t (x INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := connA.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin exclusive: %v", err)
	}
	defer func() { _, _ = connA.ExecContext(context.WithoutCancel(ctx), "ROLLBACK") }()

	connB, err := raw.Conn(ctx)
	if err != nil {
		t.Fatalf("conn B: %v", err)
	}
	defer func() { _ = connB.Close() }()

	_, busyErr := connB.ExecContext(ctx, `INSERT INTO t (x) VALUES (1)`)
	if busyErr == nil {
		t.Fatal("expected write to fail while conn A holds the exclusive lock")
	}

	rec := httptest.NewRecorder()
	if !writeTxErr(rec, busyErr) {
		t.Fatal("writeTxErr must write the busy response")
	}
	res := rec.Result()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", res.StatusCode, rec.Body.String())
	}
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header on a busy response")
	}
}
