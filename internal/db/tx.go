package db

import (
	"context"
	"database/sql"
	"errors"
)

// ErrTxHandled aborts the transaction without an error. The callback returns
// it when it already produced its own outcome (e.g. the HTTP response), so
// there is nothing left to report: the transaction rolls back and
// WithImmediateTx returns nil.
var ErrTxHandled = errors.New("transaction handled by caller")

// WithImmediateTx runs fn inside a BEGIN IMMEDIATE transaction on a dedicated
// connection, so concurrent writers serialize instead of colliding. A nil
// return commits; ErrTxHandled rolls back and reports success; any other
// error rolls back and propagates.
func WithImmediateTx(ctx context.Context, db *sql.DB, fn func(conn *sql.Conn) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}

	if err := fn(conn); err != nil {
		if errors.Is(err, ErrTxHandled) {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
			return nil
		}
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		return err
	}

	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}
