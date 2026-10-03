package handlers

import (
	"context"
	"database/sql"
	"errors"
	"github.com/pinolrent/pinolrent-api/internal/auth"
	"net/http"
	"strconv"
	"strings"
	"time"
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

// AdminGetUser returns the profile of a single user.
func (a *API) AdminGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
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

	var changed bool
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var current sql.NullInt64
		if err := conn.QueryRowContext(ctx,
			`SELECT suspended_at FROM users WHERE id = ?`, id).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "user not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		if *in.Suspended {
			if !current.Valid {
				if _, err := conn.ExecContext(ctx,
					`UPDATE users SET suspended_at = ? WHERE id = ?`, time.Now().Unix(), id); err != nil {
					serverError(w, err)
					return errTxHandled
				}
				if err := a.auditAction(ctx, conn, actor.ID, auditActionUserSuspend, targetUsers, id, ""); err != nil {
					serverError(w, err)
					return errTxHandled
				}
				changed = true
			}
		} else {
			if current.Valid {
				if _, err := conn.ExecContext(ctx,
					`UPDATE users SET suspended_at = NULL WHERE id = ?`, id); err != nil {
					serverError(w, err)
					return errTxHandled
				}
				if err := a.auditAction(ctx, conn, actor.ID, auditActionUserUnsuspend, targetUsers, id, ""); err != nil {
					serverError(w, err)
					return errTxHandled
				}
				changed = true
			}
		}
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if !changed {
		// Idempotent: setting the same state is a no-op; return 200 to be safe.
		// Re-fetch current for completeness.
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
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
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

	var changed bool
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var email string
		if err := conn.QueryRowContext(ctx,
			`SELECT email FROM users WHERE id = ?`, id).Scan(&email); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "user not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		if *in.Seller {
			res, err := conn.ExecContext(ctx,
				`INSERT OR IGNORE INTO user_roles (user_id, role) VALUES (?, 'seller')`, id)
			if err != nil {
				serverError(w, err)
				return errTxHandled
			}
			rows, err := res.RowsAffected()
			if err != nil {
				serverError(w, err)
				return errTxHandled
			}
			if rows > 0 {
				if err := a.auditAction(ctx, conn, actor.ID, auditActionUserRoleGrantSeller, targetUsers, id, ""); err != nil {
					serverError(w, err)
					return errTxHandled
				}
				changed = true
			}
		} else {
			res, err := conn.ExecContext(ctx,
				`DELETE FROM user_roles WHERE user_id = ? AND role = 'seller'`, id)
			if err != nil {
				serverError(w, err)
				return errTxHandled
			}
			rows, err := res.RowsAffected()
			if err != nil {
				serverError(w, err)
				return errTxHandled
			}
			if rows > 0 {
				if err := a.auditAction(ctx, conn, actor.ID, auditActionUserRoleRevokeSeller, targetUsers, id, ""); err != nil {
					serverError(w, err)
					return errTxHandled
				}
				changed = true
			}
		}
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	_ = changed

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

// AdminListCars lists all cars in the system, including inactive ones. This is
// an administrative view: the ownership constraints that limit ListMyCars do
// not apply.
func (a *API) AdminListCars(w http.ResponseWriter, r *http.Request) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	clauses := []string{"1=1"}
	args := []any{}
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); s != "" {
		clauses = append(clauses, "(lower(c.name) LIKE ? OR CAST(c.id AS TEXT)=?)")
		args = append(args, "%"+s+"%", s)
	}
	if s := r.URL.Query().Get("owner_id"); s != "" {
		oid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid owner_id")
			return
		}
		clauses = append(clauses, "c.owner_id = ?")
		args = append(args, oid)
	}
	if s := r.URL.Query().Get("active"); s != "" {
		v := strings.ToLower(s)
		if v != "true" && v != "false" {
			writeError(w, http.StatusBadRequest, "invalid active")
			return
		}
		active := 0
		if v == "true" {
			active = 1
		}
		clauses = append(clauses, "c.active = ?")
		args = append(args, active)
	}

	var total int64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM cars c WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT `+carColumnsQualified+`, u.email
		 FROM cars c
		 LEFT JOIN users u ON u.id = c.owner_id
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY c.id ASC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	type outCar struct {
		ID          int64  `json:"id"`
		OwnerID     int64  `json:"owner_id"`
		Name        string `json:"name"`
		PhotoURL    string `json:"photo_url,omitempty"`
		PricePerDay int64  `json:"price_per_day"`
		Active      bool   `json:"active"`
		OwnerEmail  string `json:"owner_email,omitempty"`
	}
	out := make([]outCar, 0, limit)
	for rows.Next() {
		var id int64
		var ownerID int64
		var name string
		var photoURL string
		var price int64
		var active int
		var ownerEmail sql.NullString
		if err := rows.Scan(&id, &ownerID, &name, &photoURL, &price, &active, &ownerEmail); err != nil {
			serverError(w, err)
			return
		}
		oc := outCar{ID: id, OwnerID: ownerID, Name: name, PhotoURL: photoURL, PricePerDay: price, Active: active == 1}
		if ownerEmail.Valid {
			oc.OwnerEmail = ownerEmail.String
		}
		out = append(out, oc)
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

// AdminPatchCar updates any car in the system, without the ownership check
// and without blocking deactivation when there are future reservations. The
// administrative override is deliberate: an administrator must be able to
// unpublish a problematic car regardless of its booking history.
func (a *API) AdminPatchCar(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.CurrentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid car id")
		return
	}

	var in struct {
		Name        *string `json:"name"`
		PhotoURL    *string `json:"photo_url"`
		PricePerDay *int64  `json:"price_per_day"`
		Active      *bool   `json:"active"`
	}
	if err := decodeBody(w, r, &in); err != nil {
		writeBodyErr(w, err)
		return
	}
	if in.Name == nil && in.PhotoURL == nil && in.PricePerDay == nil && in.Active == nil {
		writeError(w, http.StatusBadRequest, "no fields to update")
		return
	}
	if in.Name != nil {
		name, msg := normalizeCarName(*in.Name)
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		*in.Name = name
	}
	if in.PricePerDay != nil {
		if msg := validateCarPrice(*in.PricePerDay); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}
	if in.PhotoURL != nil {
		if msg := validateCarPhotoURL(*in.PhotoURL); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
	}

	var updated bool
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var curName, curPhoto string
		var curPrice int64
		var curActive int
		if err := conn.QueryRowContext(ctx,
			`SELECT name, photo_url, price_per_day, active FROM cars WHERE id = ?`, id).Scan(&curName, &curPhoto, &curPrice, &curActive); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "car not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}

		if in.Name != nil {
			curName = *in.Name
		}
		if in.PhotoURL != nil {
			curPhoto = *in.PhotoURL
		}
		if in.PricePerDay != nil {
			curPrice = *in.PricePerDay
		}
		if in.Active != nil {
			if *in.Active {
				curActive = 1
			} else {
				curActive = 0
			}
		}
		if _, err := conn.ExecContext(ctx,
			`UPDATE cars SET name=?, photo_url=?, price_per_day=?, active=? WHERE id=?`,
			curName, curPhoto, curPrice, curActive, id); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		if err := a.auditAction(ctx, conn, actor.ID, auditActionCarUpdate, targetCars, id, ""); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		updated = true
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if !updated {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AdminDeleteCar removes any car in the system, but only if it never had
// reservations (same guard as the seller path). The administrator can delete a
// listing without being its owner.
func (a *API) AdminDeleteCar(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.CurrentUser(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid car id")
		return
	}

	deleted := false
	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var exists int
		if err := conn.QueryRowContext(ctx, `SELECT 1 FROM cars WHERE id = ?`, id).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, "car not found")
				return errTxHandled
			}
			serverError(w, err)
			return errTxHandled
		}
		var reservations int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM reservations WHERE car_id = ?`, id).Scan(&reservations); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		if reservations > 0 {
			writeError(w, http.StatusConflict, "car has reservations, cannot delete")
			return errTxHandled
		}
		if _, err := conn.ExecContext(ctx, `DELETE FROM cars WHERE id = ?`, id); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		if err := a.auditAction(ctx, conn, actor.ID, auditActionCarDelete, targetCars, id, ""); err != nil {
			serverError(w, err)
			return errTxHandled
		}
		deleted = true
		return nil
	})
	if err != nil {
		serverError(w, err)
		return
	}
	if !deleted {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
