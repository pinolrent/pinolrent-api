package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestWithImmediateTxCommits(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	ctx := context.Background()

	err = WithImmediateTx(ctx, d, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, `INSERT INTO users (email, password_hash) VALUES ('tx@example.com', 'h')`)
		return err
	})
	if err != nil {
		t.Fatalf("tx: %v", err)
	}

	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = 'tx@example.com'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("row not committed, count = %d", n)
	}
}

func TestWithImmediateTxRollsBack(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	ctx := context.Background()
	boom := errors.New("boom")

	err = WithImmediateTx(ctx, d, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `INSERT INTO users (email, password_hash) VALUES ('rb@example.com', 'h')`); err != nil {
			t.Fatalf("insert: %v", err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}

	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = 'rb@example.com'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("tx not rolled back, count = %d", n)
	}
}

func TestWithImmediateTxRollsBackOnPanic(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	ctx := context.Background()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected the panic to propagate")
			}
		}()
		_ = WithImmediateTx(ctx, d, func(conn *sql.Conn) error {
			if _, err := conn.ExecContext(ctx, `INSERT INTO users (email, password_hash) VALUES ('panic@example.com', 'h')`); err != nil {
				t.Fatalf("insert: %v", err)
			}
			panic("boom")
		})
	}()

	// Without the rollback the pooled connection keeps the open transaction
	// and this next BEGIN fails with "cannot start a transaction within a
	// transaction".
	err = WithImmediateTx(ctx, d, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, `INSERT INTO users (email, password_hash) VALUES ('after-panic@example.com', 'h')`)
		return err
	})
	if err != nil {
		t.Fatalf("tx after panic: %v", err)
	}

	var rolled, kept int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = 'panic@example.com'`).Scan(&rolled); err != nil {
		t.Fatalf("count rolled back: %v", err)
	}
	if rolled != 0 {
		t.Fatalf("panicked transaction committed %d rows, want 0", rolled)
	}
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = 'after-panic@example.com'`).Scan(&kept); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if kept != 1 {
		t.Fatalf("next transaction wrote %d rows, want 1", kept)
	}
}

func TestWithImmediateTxRollsBackWhenCommitFails(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	ctx, cancel := context.WithCancel(context.Background())

	err = WithImmediateTx(ctx, d, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), `INSERT INTO users (email, password_hash) VALUES ('commit-fail@example.com', 'h')`); err != nil {
			t.Fatalf("insert: %v", err)
		}
		cancel() // the COMMIT below runs with a canceled context
		return nil
	})
	if err == nil {
		t.Fatal("expected the COMMIT to fail on a canceled context")
	}

	// The connection must be back in the pool without a transaction.
	err = WithImmediateTx(context.Background(), d, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(context.Background(), `INSERT INTO users (email, password_hash) VALUES ('after-fail@example.com', 'h')`)
		return err
	})
	if err != nil {
		t.Fatalf("tx after failed commit: %v", err)
	}
}

func TestWithImmediateTxPropagatesError(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	ctx := context.Background()
	boom := errors.New("boom")

	err = WithImmediateTx(ctx, d, func(_ *sql.Conn) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
