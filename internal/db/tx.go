package db

import (
	"context"
	"database/sql"
)

// WithImmediateTx runs fn inside a BEGIN IMMEDIATE transaction on a dedicated
// connection, so concurrent writers serialize instead of colliding. A nil
// return commits; any error rolls back and propagates, so callbacks return
// business-rule outcomes as errors and let the caller map them.
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
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		return err
	}

	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}
