package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pinolrent/pinolrent-api/internal/db"
)

// SyncAdminRoles makes the allow-list the single source of truth for the admin
// role: every listed account holds it, and no other account does. It runs at
// startup, so dropping an address from the list revokes that account's
// administrator access on the next boot, and there is no API that can grant
// the role behind the allow-list's back.
//
// It returns how many accounts gained and lost the role. Addresses are matched
// on lower(email) because that is the expression the unique index enforces, so
// two spellings differing only in casing are one account.
//
// It uses a plain deferred transaction instead of db.WithImmediateTx on
// purpose: it runs once at startup with no concurrent writers, so there is
// nothing to serialize against.
func (a *Auth) SyncAdminRoles(ctx context.Context, emails []string) (granted, revoked int64, err error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	normalized := make([]string, 0, len(emails))
	for _, e := range emails {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(e)))
	}
	list, err := json.Marshal(normalized)
	if err != nil {
		return 0, 0, fmt.Errorf("encode admin allow-list: %w", err)
	}

	// The grant runs first, so an account cannot be revoked in the same pass
	// that grants it. The allow-list travels as one JSON array argument and
	// json_each expands it, which keeps the statement free of any
	// concatenation.
	if len(emails) > 0 {
		res, execErr := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO user_roles (user_id, role)
			 SELECT id, ? FROM users
			 WHERE lower(email) IN (SELECT value FROM json_each(?))`, db.RoleAdmin, list)
		if execErr != nil {
			return 0, 0, fmt.Errorf("grant admin role: %w", execErr)
		}
		if granted, err = res.RowsAffected(); err != nil {
			return 0, 0, err
		}
	}

	// Every administrator not on the list loses the role. With an empty list
	// that revokes all of them, which is what an unconfigured deployment means.
	revoke := `DELETE FROM user_roles WHERE role = ?`
	revokeArgs := []any{db.RoleAdmin}
	if len(emails) > 0 {
		revoke += ` AND user_id NOT IN (
			SELECT id FROM users WHERE lower(email) IN (SELECT value FROM json_each(?)))`
		revokeArgs = append(revokeArgs, list)
	}
	res, err := tx.ExecContext(ctx, revoke, revokeArgs...)
	if err != nil {
		return 0, 0, fmt.Errorf("revoke admin role: %w", err)
	}
	if revoked, err = res.RowsAffected(); err != nil {
		return 0, 0, err
	}

	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	if granted > 0 || revoked > 0 {
		slog.Info("admin roles synced", "granted", granted, "revoked", revoked, "allowlist", len(emails))
	}
	return granted, revoked, nil
}
