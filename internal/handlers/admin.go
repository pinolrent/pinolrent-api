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

	// #nosec G202 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
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
			}
		}
		return nil
	})
	if err != nil {
		serverError(w, err)
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

	err = withImmediateTx(r.Context(), a.DB, func(conn *sql.Conn) error {
		ctx := r.Context()
		var exists int
		if err := conn.QueryRowContext(ctx,
			`SELECT 1 FROM users WHERE id = ?`, id).Scan(&exists); err != nil {
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
			}
		}
		return nil
	})
	if err != nil {
		serverError(w, err)
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
	// #nosec G202 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM cars c WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	// #nosec G202 -- same as the count above.
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

const adminReservationSelect = `
SELECT r.id, r.user_id, r.car_id, r.start_date, r.end_date, r.status,
       u.email, c.name
FROM reservations r
LEFT JOIN users u ON u.id = r.user_id
LEFT JOIN cars c ON c.id = r.car_id
`

// AdminListReservations returns all reservations on the platform.
func (a *API) AdminListReservations(w http.ResponseWriter, r *http.Request) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	clauses := []string{"1=1"}
	args := []any{}
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status"))); s != "" {
		// The reservations CHECK only allows these three. A reservation is
		// paid when its payment row is approved, not through its own status.
		if s != "pending" && s != "confirmed" && s != "cancelled" {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		clauses = append(clauses, "r.status = ?")
		args = append(args, s)
	}
	if s := r.URL.Query().Get("user_id"); s != "" {
		uid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid user_id")
			return
		}
		clauses = append(clauses, "r.user_id = ?")
		args = append(args, uid)
	}
	if s := r.URL.Query().Get("car_id"); s != "" {
		cid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid car_id")
			return
		}
		clauses = append(clauses, "r.car_id = ?")
		args = append(args, cid)
	}

	var total int64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM reservations r WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	rows, err := a.DB.QueryContext(r.Context(),
		adminReservationSelect+`
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY r.id ASC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	out := make([]adminReservationOut, 0, limit)
	for rows.Next() {
		var r adminReservationOut
		var buyerEmail, carName sql.NullString
		if err := rows.Scan(&r.ID, &r.UserID, &r.CarID, &r.StartDate, &r.EndDate, &r.Status, &buyerEmail, &carName); err != nil {
			serverError(w, err)
			return
		}
		if buyerEmail.Valid {
			r.BuyerEmail = buyerEmail.String
		}
		if carName.Valid {
			r.CarName = carName.String
		}
		out = append(out, r)
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

type adminReservationOut struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	CarID      int64  `json:"car_id"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
	Status     string `json:"status"`
	BuyerEmail string `json:"buyer_email,omitempty"`
	CarName    string `json:"car_name,omitempty"`
}

// AdminListPayments returns all payments in the platform.
func (a *API) AdminListPayments(w http.ResponseWriter, r *http.Request) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	clauses := []string{"1=1"}
	args := []any{}
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status"))); s != "" {
		if s != "pending" && s != "approved" && s != "rejected" {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		clauses = append(clauses, "p.status = ?")
		args = append(args, s)
	}
	if s := r.URL.Query().Get("reservation_id"); s != "" {
		rid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid reservation_id")
			return
		}
		clauses = append(clauses, "p.reservation_id = ?")
		args = append(args, rid)
	}

	var total int64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM payments p WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	// #nosec G202 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT p.id, p.reservation_id, p.method, p.status, p.proof_url, r.user_id, r.car_id, u.email
		 FROM payments p
		 LEFT JOIN reservations r ON r.id = p.reservation_id
		 LEFT JOIN users u ON u.id = r.user_id
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY p.id ASC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer func() { _ = rows.Close() }()

	type outPay struct {
		ID            int64  `json:"id"`
		ReservationID int64  `json:"reservation_id"`
		Method        string `json:"method"`
		Status        string `json:"status"`
		ProofURL      string `json:"proof_url,omitempty"`
		UserID        int64  `json:"user_id,omitempty"`
		CarID         int64  `json:"car_id,omitempty"`
		BuyerEmail    string `json:"buyer_email,omitempty"`
	}
	out := make([]outPay, 0, limit)
	for rows.Next() {
		var id, rid, uid, cid int64
		var method, status, proof string
		var buyerEmail sql.NullString
		if err := rows.Scan(&id, &rid, &method, &status, &proof, &uid, &cid, &buyerEmail); err != nil {
			serverError(w, err)
			return
		}
		po := outPay{ID: id, ReservationID: rid, Method: method, Status: status, ProofURL: proof, UserID: uid, CarID: cid}
		if buyerEmail.Valid {
			po.BuyerEmail = buyerEmail.String
		}
		out = append(out, po)
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

// AdminStats returns high-level platform metrics for administrators.
func (a *API) AdminStats(w http.ResponseWriter, r *http.Request) {
	var totalUsers, totalSellers, totalAdmins, suspended int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users`).Scan(&totalUsers); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT user_id) FROM user_roles WHERE role='seller'`).Scan(&totalSellers); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(DISTINCT user_id) FROM user_roles WHERE role='admin'`).Scan(&totalAdmins); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM users WHERE suspended_at IS NOT NULL`).Scan(&suspended); err != nil {
		serverError(w, err)
		return
	}

	var activeCars int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM cars WHERE active=1`).Scan(&activeCars); err != nil {
		serverError(w, err)
		return
	}

	var pendingRes, confirmedRes, cancelledRes int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM reservations WHERE status='pending'`).Scan(&pendingRes); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM reservations WHERE status='confirmed'`).Scan(&confirmedRes); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM reservations WHERE status='cancelled'`).Scan(&cancelledRes); err != nil {
		serverError(w, err)
		return
	}

	var pendingPay, approvedPay, rejectedPay int64
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM payments WHERE status='pending'`).Scan(&pendingPay); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM payments WHERE status='approved'`).Scan(&approvedPay); err != nil {
		serverError(w, err)
		return
	}
	if err := a.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM payments WHERE status='rejected'`).Scan(&rejectedPay); err != nil {
		serverError(w, err)
		return
	}
	var approvedAmount sql.NullInt64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COALESCE(SUM(c.price_per_day * (julianday(r.end_date) - julianday(r.start_date))), 0)
		 FROM payments p
		 JOIN reservations r ON r.id = p.reservation_id
		 JOIN cars c ON c.id = r.car_id
		 WHERE p.status='approved'`).Scan(&approvedAmount); err != nil {
		serverError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"users": map[string]int64{
			"total":     totalUsers,
			"sellers":   totalSellers,
			"admins":    totalAdmins,
			"suspended": suspended,
		},
		"cars": map[string]int64{
			"active": activeCars,
		},
		"reservations": map[string]int64{
			"pending":   pendingRes,
			"confirmed": confirmedRes,
			"cancelled": cancelledRes,
		},
		"payments": map[string]any{
			"pending":        pendingPay,
			"approved":       approvedPay,
			"rejected":       rejectedPay,
			"approved_total": approvedAmount.Int64,
		},
	})
}

// AdminListAudit returns the audit log for administrator actions.
func (a *API) AdminListAudit(w http.ResponseWriter, r *http.Request) {
	limit, offset, errMsg := paginate(r)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	clauses := []string{"1=1"}
	args := []any{}
	if s := r.URL.Query().Get("actor_id"); s != "" {
		aid, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid actor_id")
			return
		}
		clauses = append(clauses, "a.actor_id = ?")
		args = append(args, aid)
	}
	if s := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("action"))); s != "" {
		clauses = append(clauses, "lower(a.action) = ?")
		args = append(args, s)
	}

	var total int64
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM admin_audit_log a WHERE `+strings.Join(clauses, " AND "), args...).Scan(&total); err != nil {
		serverError(w, err)
		return
	}

	// #nosec G202 -- clauses are built here from fixed fragments with placeholders;
	// every value from the query string is bound as a parameter.
	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT a.id, a.actor_id, a.action, a.target_type, a.target_id, a.detail, a.created_at, u.email
		 FROM admin_audit_log a
		 LEFT JOIN users u ON u.id = a.actor_id
		 WHERE `+strings.Join(clauses, " AND ")+`
		 ORDER BY a.id DESC
		 LIMIT ? OFFSET ?`, append(args, limit, offset)...)
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
