package handlers

import (
	"context"
	"database/sql"
	"errors"
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
