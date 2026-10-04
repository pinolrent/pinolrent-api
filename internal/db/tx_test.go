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

func TestWithImmediateTxHandledRollsBack(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = d.Close() }()
	ctx := context.Background()

	err = WithImmediateTx(ctx, d, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `INSERT INTO users (email, password_hash) VALUES ('rb@example.com', 'h')`); err != nil {
			t.Fatalf("insert: %v", err)
		}
		return ErrTxHandled
	})
	if err != nil {
		t.Fatalf("handled tx should return nil, got %v", err)
	}

	var n int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = 'rb@example.com'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("handled tx not rolled back, count = %d", n)
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
