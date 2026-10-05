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
	auditActionUserSuspend                  = "user.suspend"
	auditActionUserUnsuspend                = "user.unsuspend"
	auditActionUserRoleGrantSeller          = "user.role_grant_seller"
	auditActionUserRoleRevokeSeller         = "user.role_revoke_seller"
	auditActionCarUpdate                    = "car.update"
	auditActionCarDelete                    = "car.delete"
	auditActionReservationAccept            = "reservation.accept"
	auditActionReservationReject            = "reservation.reject"
	auditActionReservationCancel            = "reservation.cancel"
	auditActionReservationConfirm           = "reservation.confirm"
	auditActionReservationRequestCorrection = "reservation.request_correction"
)

type targetType string

const (
	targetUsers        = targetType("users")
	targetCars         = targetType("cars")
	targetReservations = targetType("reservations")
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

// AdminListAudit returns the audit log for administrator actions.
func (a *API) AdminListAudit(w http.ResponseWriter, r *http.Request) {
	type outAudit struct {
		ID         int64  `json:"id"`
		ActorID    int64  `json:"actor_id"`
		ActorEmail string `json:"actor_email,omitempty"`
		Action     string `json:"action"`
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Detail     string `json:"detail,omitempty"`
		CreatedAt  string `json:"created_at"`
	}
	listPage(a, w, r,
		`SELECT COUNT(*) FROM admin_audit_log a`,
		`SELECT a.id, a.actor_id, a.action, a.target_type, a.target_id, a.detail, a.created_at, u.email
		 FROM admin_audit_log a
		 LEFT JOIN users u ON u.id = a.actor_id`,
		`ORDER BY a.id DESC`,
		func(f *filter) string {
			if aid, present, errMsg := queryID(r, "actor_id"); errMsg != "" {
				return errMsg
			} else if present {
				f.add("a.actor_id = ?", aid)
			}
			if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("action"))); s != "" {
				f.add("lower(a.action) = ?", s)
			}
			return ""
		},
		func(row rowScanner) (outAudit, error) {
			var oa outAudit
			var actorEmail sql.NullString
			if err := row.Scan(&oa.ID, &oa.ActorID, &oa.Action, &oa.TargetType, &oa.TargetID, &oa.Detail, &oa.CreatedAt, &actorEmail); err != nil {
				return oa, err
			}
			if actorEmail.Valid {
				oa.ActorEmail = actorEmail.String
			}
			return oa, nil
		})
}
