package handlers

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestWithImmediateTxCommits(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()

	err := withImmediateTx(ctx, a.DB, func(conn *sql.Conn) error {
		_, err := conn.ExecContext(ctx, `INSERT INTO users (email, password_hash, role) VALUES ('tx@example.com', 'h', 'buyer')`)
		return err
	})
	if err != nil {
		t.Fatalf("tx: %v", err)
	}

	var n int
	if err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = 'tx@example.com'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("row not committed, count = %d", n)
	}
}

func TestWithImmediateTxHandledRollsBack(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()

	err := withImmediateTx(ctx, a.DB, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `INSERT INTO users (email, password_hash, role) VALUES ('rb@example.com', 'h', 'buyer')`); err != nil {
			t.Fatalf("insert: %v", err)
		}
		return errTxHandled
	})
	if err != nil {
		t.Fatalf("handled tx should return nil, got %v", err)
	}

	var n int
	if err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = 'rb@example.com'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("handled tx not rolled back, count = %d", n)
	}
}

func TestWithImmediateTxPropagatesError(t *testing.T) {
	a := newTestAPI(t)
	ctx := context.Background()
	boom := errors.New("boom")

	err := withImmediateTx(ctx, a.DB, func(_ *sql.Conn) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}
