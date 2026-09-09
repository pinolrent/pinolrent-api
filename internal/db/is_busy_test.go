package db

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// The sqlite.Error struct has unexported fields, so detection is exercised
// against real driver errors rather than hand-built ones.

func TestIsBusyError(t *testing.T) {
	if isBusyError(nil) {
		t.Fatal("nil should not be a busy error")
	}
	if isBusyError(errors.New("database is locked")) {
		t.Fatal("plain error with busy-like text should not be a busy error")
	}
}

func TestIsBusyErrorReal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.db")
	raw, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(0)")
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
	// Hold an exclusive lock so any write from another connection
	// fails immediately with SQLITE_BUSY (busy_timeout is 0).
	if _, err := connA.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin exclusive: %v", err)
	}
	defer func() { _, _ = connA.ExecContext(context.WithoutCancel(ctx), "ROLLBACK") }()

	connB, err := raw.Conn(ctx)
	if err != nil {
		t.Fatalf("conn B: %v", err)
	}
	defer func() { _ = connB.Close() }()

	_, err = connB.ExecContext(ctx, `INSERT INTO t (x) VALUES (1)`)
	if err == nil {
		t.Fatal("expected write to fail while conn A holds the exclusive lock")
	}
	if !isBusyError(err) {
		t.Fatalf("expected busy error, got: %v", err)
	}
}
