package handlers

import (
	"context"
	"database/sql"
	"errors"
)

var errTxHandled = errors.New("response already sent")

func withImmediateTx(ctx context.Context, db *sql.DB, fn func(conn *sql.Conn) error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}

	if err := fn(conn); err != nil {
		if errors.Is(err, errTxHandled) {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
			return nil
		}
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		return err
	}

	_, err = conn.ExecContext(ctx, "COMMIT")
	return err
}
