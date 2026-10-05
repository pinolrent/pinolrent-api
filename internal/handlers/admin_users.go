package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
	"github.com/pinolrent/pinolrent-api/internal/models"
)

// AdminListUsers returns the platform user list for an administrator.
func (a *API) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))

	// outUser is the account with its suspension instant: the embedded
	// models.User keeps the account shape in one place. SuspendedAt shadows
	// User.SuspendedAt (never serialized) with the serialized form.
	type outUser struct {
		models.User
		SuspendedAt int64 `json:"suspended_at,omitempty"`
	}
	listPage(a, w, r,
		`SELECT COUNT(DISTINCT u.id) FROM users u`,
		`SELECT u.id, u.email, u.phone, u.suspended_at, GROUP_CONCAT(ur.role) as roles
		 FROM users u
		 LEFT JOIN user_roles ur ON ur.user_id = u.id`,
		`GROUP BY u.id, u.email, u.phone, u.suspended_at ORDER BY u.id ASC`,
		func(f *filter) string {
			if q != "" {
				f.add("(lower(u.email) LIKE ? OR CAST(u.id AS TEXT) = ?)", "%"+q+"%", q)
			}
			if role != "" {
				f.add("EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = u.id AND r.role = ?)", role)
			}
			return ""
		},
		func(row rowScanner) (outUser, error) {
			var u outUser
			var suspended sql.NullInt64
			var rolesStr sql.NullString
			if err := row.Scan(&u.ID, &u.Email, &u.Phone, &suspended, &rolesStr); err != nil {
				return u, err
			}
			if rolesStr.Valid && rolesStr.String != "" {
				u.Roles = strings.Split(rolesStr.String, ",")
			}
			if suspended.Valid {
				u.SuspendedAt = suspended.Int64
			}
			return u, nil
		})
}

// AdminGetUser returns the profile of a single user.
func (a *API) AdminGetUser(w http.ResponseWriter, r *http.Request) {
	id, errMsg := pathID(r, "user")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	var email, phone string
	var suspended sql.NullInt64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT email, phone, suspended_at FROM users WHERE id = ?`, id).Scan(&email, &phone, &suspended); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		serverError(w, err)
		return
	}
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT role FROM user_roles WHERE user_id = ? ORDER BY role`, id)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			serverError(w, err)
			return
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	out := map[string]any{
		"id":    id,
		"email": email,
		"phone": phone,
		"roles": roles,
	}
	if suspended.Valid {
		out["suspended_at"] = suspended.Int64
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminPatchUser toggles the account's suspension. An administrator cannot
// suspend themselves: that would remove the only path to re-enable the
// platform and is rejected with 400.
func (a *API) AdminPatchUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.CurrentUser(r.Context())
	id, errMsg := pathID(r, "user")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}
	if actor.ID == id {
		writeError(w, http.StatusBadRequest, "cannot suspend yourself")
		return
	}

	var in struct {
		Suspended *bool `json:"suspended"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	if in.Suspended == nil {
		writeError(w, http.StatusBadRequest, "suspended is required")
		return
	}

	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var current sql.NullInt64
		if err := conn.QueryRowContext(ctx,
			`SELECT suspended_at FROM users WHERE id = ?`, id).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "user not found"}
			}
			return err
		}
		if *in.Suspended {
			if !current.Valid {
				if _, err := conn.ExecContext(ctx,
					`UPDATE users SET suspended_at = ? WHERE id = ?`, time.Now().Unix(), id); err != nil {
					return err
				}
				if err := a.auditAction(ctx, conn, actor.ID, auditActionUserSuspend, targetUsers, id, ""); err != nil {
					return err
				}
			}
		} else {
			if current.Valid {
				if _, err := conn.ExecContext(ctx,
					`UPDATE users SET suspended_at = NULL WHERE id = ?`, id); err != nil {
					return err
				}
				if err := a.auditAction(ctx, conn, actor.ID, auditActionUserUnsuspend, targetUsers, id, ""); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if writeTxErr(w, err) {
		return
	}

	// Return current state.
	var email, phone string
	var suspended sql.NullInt64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT email, phone, suspended_at FROM users WHERE id = ?`, id).Scan(&email, &phone, &suspended); err != nil {
		serverError(w, err)
		return
	}
	out := map[string]any{
		"id":    id,
		"email": email,
		"phone": phone,
	}
	if suspended.Valid {
		out["suspended_at"] = suspended.Int64
	}
	writeJSON(w, http.StatusOK, out)
}

// AdminPatchUserRoles grants or revokes the seller role. The admin role itself
// is never touched through this endpoint: it is only granted by the allow-list
// at startup and at registration, so there is no public path to escalate to
// admin.
func (a *API) AdminPatchUserRoles(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.CurrentUser(r.Context())
	id, errMsg := pathID(r, "user")
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	var in struct {
		Seller *bool `json:"seller"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	if in.Seller == nil {
		writeError(w, http.StatusBadRequest, "seller is required")
		return
	}

	err := db.WithImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var phone string
		if err := conn.QueryRowContext(ctx,
			`SELECT phone FROM users WHERE id = ?`, id).Scan(&phone); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return &statusError{http.StatusNotFound, "user not found"}
			}
			return err
		}
		if *in.Seller && phone == "" {
			// The seller role promises a contact phone to buyers; an
			// account without one cannot hold it, however it is granted.
			return &statusError{http.StatusConflict, "user has no phone number"}
		}
		if *in.Seller {
			res, err := conn.ExecContext(ctx,
				`INSERT OR IGNORE INTO user_roles (user_id, role) VALUES (?, ?)`, id, db.RoleSeller)
			if err != nil {
				return err
			}
			rows, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if rows > 0 {
				if err := a.auditAction(ctx, conn, actor.ID, auditActionUserRoleGrantSeller, targetUsers, id, ""); err != nil {
					return err
				}
			}
		} else {
			res, err := conn.ExecContext(ctx,
				`DELETE FROM user_roles WHERE user_id = ? AND role = ?`, id, db.RoleSeller)
			if err != nil {
				return err
			}
			rows, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if rows > 0 {
				if err := a.auditAction(ctx, conn, actor.ID, auditActionUserRoleRevokeSeller, targetUsers, id, ""); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if writeTxErr(w, err) {
		return
	}

	var email string
	var phone string
	var suspended sql.NullInt64
	var rolesStr sql.NullString
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT u.email, u.phone, u.suspended_at, GROUP_CONCAT(ur.role)
		 FROM users u LEFT JOIN user_roles ur ON ur.user_id = u.id
		 WHERE u.id = ?
		 GROUP BY u.id, u.email, u.phone, u.suspended_at`, id).Scan(&email, &phone, &suspended, &rolesStr); err != nil {
		serverError(w, err)
		return
	}
	var roles []string
	if rolesStr.Valid && rolesStr.String != "" {
		roles = strings.Split(rolesStr.String, ",")
	}
	out := map[string]any{
		"id":    id,
		"email": email,
		"phone": phone,
		"roles": roles,
	}
	if suspended.Valid {
		out["suspended_at"] = suspended.Int64
	}
	writeJSON(w, http.StatusOK, out)
}
