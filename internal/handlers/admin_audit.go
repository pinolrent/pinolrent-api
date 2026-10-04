package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
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
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	var f filter
	if s := r.URL.Query().Get("actor_id"); s != "" {
		aid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid actor_id")
			return
		}
		f.add("a.actor_id = ?", aid)
	}
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("action"))); s != "" {
		f.add("lower(a.action) = ?", s)
	}

	var total int64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM admin_audit_log a WHERE `+f.where(), f.params()...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	// #nosec G202 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT a.id, a.actor_id, a.action, a.target_type, a.target_id, a.detail, a.created_at, u.email
		 FROM admin_audit_log a
		 LEFT JOIN users u ON u.id = a.actor_id
		 WHERE `+f.where()+`
		 ORDER BY a.id DESC
		 LIMIT ? OFFSET ?`, f.page(limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

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
	out := make([]outAudit, 0, limit)
	for rows.Next() {
		var id, actorID, targetID int64
		var action, ttype, detail, created string
		var actorEmail sql.NullString
		if err := rows.Scan(&id, &actorID, &action, &ttype, &targetID, &detail, &created, &actorEmail); err != nil {
			serverError(w, err)
			return
		}
		oa := outAudit{ID: id, ActorID: actorID, Action: action, TargetType: ttype, TargetID: targetID, Detail: detail, CreatedAt: created}
		if actorEmail.Valid {
			oa.ActorEmail = actorEmail.String
		}
		out = append(out, oa)
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
