package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

// Admin endpoints sit behind RequireRole("admin"). The route table attaches the
// limiter, so these handlers assume the caller is an authenticated administrator
// (not suspended, due to RequireAuth) and focus on enforcing business rules.

const (
	auditActionUserSuspend          = "user.suspend"
	auditActionUserUnsuspend        = "user.unsuspend"
	auditActionUserRoleGrantSeller  = "user.role_grant_seller"
	auditActionUserRoleRevokeSeller = "user.role_revoke_seller"
	auditActionCarUpdate            = "car.update"
	auditActionCarDelete            = "car.delete"
)

type targetType string

const (
	targetUsers = targetType("users")
	targetCars  = targetType("cars")
)

func (a *API) auditAction(ctx context.Context, conn *sql.Conn, actorID int64, action string, tt targetType, targetID int64, detail string) error {
	if actorID == 0 {
		return errors.New("audit actor missing")
	}
	if strings.TrimSpace(action) == "" {
		return errors.New("audit action missing")
	}
	_, err := conn.ExecContext(ctx,
		`INSERT INTO admin_audit_log (actor_id, action, target_type, target_id, detail)
		 VALUES (?, ?, ?, ?, ?)`, actorID, action, string(tt), targetID, strings.TrimSpace(detail))
	return err
}

// AdminListUsers returns the platform user list for an administrator.
func (a *API) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))

	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	clauses := []string{"1=1"}
	args := []any{}
	if q != "" {
		clauses = append(clauses, "(lower(u.email) LIKE ? OR CAST(u.id AS TEXT) = ?)")
		args = append(args, "%"+q+"%", q)
	}
	if role != "" {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = u.id AND r.role = ?)")
		args = append(args, role)
	}

	var total int64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(DISTINCT u.id) FROM users u WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT u.id, u.email, u.phone, u.suspended_at, GROUP_CONCAT(ur.role) as roles
		 FROM users u
		 LEFT JOIN user_roles ur ON ur.user_id = u.id
		 WHERE `+strings.Join(clauses, " AND ")+`
		 GROUP BY u.id, u.email, u.phone, u.suspended_at
		 ORDER BY u.id ASC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	type outUser struct {
		ID          int64    `json:"id"`
		Email       string   `json:"email"`
		Phone       string   `json:"phone,omitempty"`
		Roles       []string `json:"roles"`
		SuspendedAt int64    `json:"suspended_at,omitempty"`
	}
	out := make([]outUser, 0, limit)
	for rows.Next() {
		var id int64
		var email, phone string
		var suspended sql.NullInt64
		var rolesStr sql.NullString
		if err := rows.Scan(&id, &email, &phone, &suspended, &rolesStr); err != nil {
			serverError(w, err)
			return
		}
		var roles []string
		if rolesStr.Valid && rolesStr.String != "" {
			roles = strings.Split(rolesStr.String, ",")
		}
		u := outUser{ID: id, Email: email, Phone: phone, Roles: roles}
		if suspended.Valid {
			u.SuspendedAt = suspended.Int64
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
